package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/logging"
)

var errFileTruncated = errors.New("zero-length config file")

var (
	// Time
	RetryPeriod = 1 * time.Second

	// Time allowed to write a message to the CDS server.
	WriteWait = 2 * time.Second

	// Time allowed to read the next pong message from the CDS server.
	PongWait = 8 * time.Second

	// Send pings to the CDS server with this period. Must be less than PongWait.
	PingPeriod = 5 * time.Second
)

// Client represents a generic config client. Currently supported config providers: http, ws, or
// file. Configuration obtained through the client are not validated, make sure to validate on the
// receiver side.
type Client interface {
	// Load grabs a new configuration from the config client.
	Load() (*stnrv1.StunnerConfig, error)
	// Watch listens to new configurations and returns them on the channel ch. The context ctx
	// cancels the watcher.
	Watch(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error
	fmt.Stringer
}

// GetConfig returns the configuration of the running STUNner daemon.
func NewClient(origin string, id string, logger logging.LoggerFactory) (Client, error) {
	u, err := url.Parse(origin)
	if err != nil {
		return nil, fmt.Errorf("could not parse config origin address %q: %w", origin, err)
	}

	var client Client
	switch strings.ToLower(u.Scheme) {
	case "http", "ws", "https", "wss":
		client = &configDiscoveryClient{
			serverAddress: origin,
			id:            id,
			log:           logger.NewLogger("config-poller"),
		}
	default:
		client = &configFileClient{
			configFile: origin,
			id:         id,
			log:        logger.NewLogger("config-watcher"),
		}
	}

	return client, nil
}

// configFileClient is the implementation of the Client interface for config files.
type configFileClient struct {
	// configFile specifies the config file name to watch.
	configFile string
	// id is the name of the stunnerd instance.
	id string
	// log is a leveled logger used to report progress. Either Logger or Log must be specified.
	log logging.LeveledLogger
}

func (w *configFileClient) String() string {
	return fmt.Sprintf("config client using file %q", w.configFile)
}

func (w *configFileClient) Load() (*stnrv1.StunnerConfig, error) {
	b, err := os.ReadFile(w.configFile)
	if err != nil {
		return nil, fmt.Errorf("could not read config file %q: %s", w.configFile, err.Error())
	}

	if len(b) == 0 {
		return nil, errFileTruncated
	}

	return ParseConfig(b)
}

// WatchConfig watches a configuration file for changes. If no file exists at the given path,
// WatchConfig will periodically retry until the file appears.
func (w *configFileClient) Watch(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	if w.configFile == "" {
		return errors.New("uninitialized config file path")
	}

	// emit an empty config: this bootstraps stunner the default resources (above all, starts
	// the health-checker)
	w.log.Debug("bootstrapping with zero configuration")
	initConf := ZeroConfig(w.id)
	ch <- *initConf

	go func() {
		for {
			// try to watch
			if !w.configWatcher(ctx, ch) {
				return
			}

			if !w.tryWatchConfig(ctx) {
				return
			}
		}
	}()

	return nil
}

// configWatcher watches the config file and emits new configs on the specified channel. Returns
// true if further action is needed (tryWatchConfig is to be started) or false on normal exit.
func (w *configFileClient) configWatcher(ctx context.Context, ch chan<- stnrv1.StunnerConfig) bool {
	w.log.Tracef("configWatcher")

	// create a new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return true
	}
	defer watcher.Close()

	config := w.configFile
	if err := watcher.Add(config); err != nil {
		w.log.Debugf("could not add watcher for config file %q: %s", config, err.Error())
		return true
	}

	// emit an initial config
	c, err := w.Load()
	if err != nil {
		w.log.Warnf("cannot load config file: %s", err.Error())
		return true
	}

	// send a deepcopy over the channel
	confCopy := stnrv1.StunnerConfig{}
	c.DeepCopyInto(&confCopy)

	w.log.Debugf("initial config file successfully loaded from %q: %s", config, confCopy.String())

	ch <- confCopy

	// save deepcopy so that we can filter repeated events
	prev := stnrv1.StunnerConfig{}
	c.DeepCopyInto(&prev)

	for {
		select {
		case <-ctx.Done():
			return false

		case e, ok := <-watcher.Events:
			if !ok {
				w.log.Debug("config watcher event handler received invalid event")
				return true
			}

			w.log.Debugf("received watcher event: %s", e.String())

			if e.Has(fsnotify.Remove) {
				w.log.Warnf("config file deleted %q, disabling watcher", e.Op.String())

				if err := watcher.Remove(config); err != nil {
					w.log.Debugf("could not remove config file %q watcher: %s",
						config, err.Error())
				}

				return true
			}

			if !e.Has(fsnotify.Write) {
				w.log.Debugf("unhandled notify op on config file %q (ignoring): %s",
					e.Name, e.Op.String())
				continue
			}

			w.log.Debugf("loading configuration file: %s", config)
			c, err := w.Load()
			if err != nil {
				if errors.Is(err, errFileTruncated) {
					w.log.Debugf("ignoring: %s", err.Error())
					continue
				}
				w.log.Warnf("error loading config file: %s", err.Error())
				return true
			}

			// suppress repeated events
			if c.DeepEqual(&prev) {
				w.log.Debugf("ignoring recurrent notify event for the same config file")
				continue
			}

			confCopy := stnrv1.StunnerConfig{}
			c.DeepCopyInto(&confCopy)

			w.log.Debugf("config file successfully loaded from %q: %s", config, confCopy.String())

			ch <- confCopy

			// save deepcopy so that we can filter repeated events
			c.DeepCopyInto(&prev)

		case err, ok := <-watcher.Errors:
			if !ok {
				w.log.Debugf("config watcher error handler received invalid error")
				return true
			}

			w.log.Debugf("watcher error, deactivating watcher: %s", err.Error())

			if err := watcher.Remove(config); err != nil {
				w.log.Debugf("could not remove config file %q watcher: %s",
					config, err.Error())
			}

			return true
		}
	}
}

// tryWatchConfig runs a timer to look for the config file at the given path and returns it
// immediately once found. Returns true if further action is needed (configWatcher has to be
// started) or false on normal exit.
func (w *configFileClient) tryWatchConfig(ctx context.Context) bool {
	w.log.Tracef("tryWatchConfig")
	config := w.configFile

	ticker := time.NewTicker(RetryPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false

		case <-ticker.C:
			w.log.Debugf("trying to read config file %q from periodic timer",
				config)

			// check if config file exists and it is readable
			if _, err := os.Stat(config); errors.Is(err, os.ErrNotExist) {
				w.log.Debugf("config file %q does not exist", config)

				// report status in every 10th second
				if time.Now().Second()%10 == 0 {
					w.log.Warnf("waiting for config file %q", config)
				}

				continue
			}

			return true
		}
	}
}

// configDiscoveryClient is the the implementation of the config discovery service.
type configDiscoveryClient struct {
	// serverAddress is the URL of the config discovery server.
	serverAddress string
	// Id is the name of the stunnerd instance that is used to bootstrap the connection
	// poller. Set to namespace/name of the pod when using the stunner gateway operator for
	// config discovery.
	id string
	// Log is a leveled logger used to report progress.
	log logging.LeveledLogger
}

func (p *configDiscoveryClient) String() string {
	return fmt.Sprintf("config discovery service using server %q", p.serverAddress)
}

func (p *configDiscoveryClient) Load() (*stnrv1.StunnerConfig, error) {
	location, _, err := getConfigDiscoveryLocation(p.serverAddress, p.id, false)
	if err != nil {
		return nil, err
	}

	resp, err := http.Get(location)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid HTTP response status: %s", resp.Status)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if len(body) == 0 {
		return nil, errFileTruncated
	}

	// fmt.Println("++++++++++++++++++++")
	// fmt.Println(string(body))

	return ParseConfig(body)
}

// Watch polls a config discovery server for a configuration file by sending a config request and
// then waits for the server to push a valid `StunnerConfig`. Use the `context` to cancel the
// watcher.
func (p *configDiscoveryClient) Watch(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	_, _, err := getConfigDiscoveryLocation(p.serverAddress, p.id, true)
	if err != nil {
		return err
	}

	// Note: we do not emit an initial config but rather wait for the CDS server to send one,
	// so that pod will not be able to bootstrap the healthchecker and keep on restarting until
	// it finds the CDS server

	go func() {
		for {
			// try to watch
			if err := p.configPoller(ctx, ch); err != nil {
				p.log.Errorf("config file discovery service: %s", err.Error())
			} else {
				// context got cancelled
				return
			}

			// wait between each attempt
			time.Sleep(RetryPeriod)
		}
	}()

	return nil
}

func (p *configDiscoveryClient) configPoller(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	p.log.Tracef("configPoller: trying to open connection to config discovery server at %q", p.serverAddress)

	location, origin, _ := getConfigDiscoveryLocation(p.serverAddress, p.id, true)
	header := http.Header{}
	header.Set("origin", origin)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, location, header)
	if err != nil {
		return err
	}

	p.log.Infof("connection sucessfully opened to config discovery server at %q", location)

	// this will close the poller goroutine
	defer conn.Close()

	// pinger
	resCh := make(chan stnrv1.StunnerConfig, 16)
	errCh := make(chan error, 1)

	pingTicker := time.NewTicker(PingPeriod)
	closePinger := make(chan any)
	defer close(closePinger)

	go func() {
		defer pingTicker.Stop()
		for {
			select {
			case <-pingTicker.C:
				// p.log.Tracef("++++ PING ++++ for CDS server %q at client %q", location, p.id)
				conn.SetWriteDeadline(time.Now().Add(WriteWait)) //nolint:errcheck
				if err := conn.WriteMessage(websocket.PingMessage, []byte("keepalive")); err != nil {
					errCh <- fmt.Errorf("could not ping CDS server at %q: %w",
						conn.RemoteAddr(), err)
					return
				}
			case <-closePinger:
				p.log.Tracef("closing ping handler to config discovery server at %q at client %q",
					location, p.id)
				return
			}
		}
	}()

	// poller
	go func() {
		defer close(resCh)
		defer close(errCh)

		// the next pong must arrive within the PongWait period
		conn.SetReadDeadline(time.Now().Add(PongWait)) //nolint:errcheck
		// reinit the deadline when we get a pong
		conn.SetPongHandler(func(string) error {
			// p.log.Tracef("++++ PONG ++++ from CDS server %q at client %q", location, p.id)
			conn.SetReadDeadline(time.Now().Add(PongWait)) //nolint:errcheck
			return nil
		})

		for {
			// ping-pong deadline misses will end up being caught here as a read beyond
			// the deadline
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}

			if msgType != websocket.TextMessage {
				errCh <- fmt.Errorf("unexpected message type (code: %d) from client %q",
					msgType, conn.RemoteAddr().String())
				return
			}

			if len(msg) == 0 {
				p.log.Warn("ignoring zero-length config config fil")
				continue
			}

			// fmt.Println("++++++++++++++++++++")
			// fmt.Println(string(msg))
			// fmt.Println("++++++++++++++++++++")

			c, err := ParseConfig(msg)
			if err != nil {
				// assume it is a YAML/JSON syntax error: report and ignore
				p.log.Warnf("could not parse config: %s", err.Error())
				continue
			}

			confCopy := stnrv1.StunnerConfig{}
			c.DeepCopyInto(&confCopy)

			p.log.Debugf("new config received from %q: %s", p.serverAddress, confCopy.String())

			resCh <- confCopy
		}
	}()

	// wait fo cancel
	for {
		select {
		case <-ctx.Done():
			// cancel: normal return
			closePinger <- struct{}{}

			return nil
		case err := <-errCh:
			// error: return it
			closePinger <- struct{}{}

			return err
		case conf := <-resCh:
			// new config: pass it along and move on
			p.log.Debugf("new config available: %s", conf.String())
			ch <- conf

			continue
		}
	}
}

// getConfigLocation returns a valid URL from config server address, either for a single HTTP GET
// to query the config discovery server for a single config file or a websocket URL and client for
// polling config file updates
func getConfigDiscoveryLocation(addr, id string, ws bool) (string, string, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return "", "", fmt.Errorf("invalid config discovery server URL %q: %w", addr, err)
	}

	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", "", fmt.Errorf("invalid config discovery query server URL %q: %w", addr, err)
	}

	// add our id as a query parameter
	q.Set("id", id)
	u.RawQuery = q.Encode()

	// TODO: share between server and client
	u.Path = "/api/v1/config"
	if ws {
		u.Scheme = "ws"
		u.Path = u.Path + "/watch"
	} else {
		u.Scheme = "http"
	}

	// target URL
	location := u.String()

	// client
	u.Scheme = "http"
	u.RawQuery = ""
	client := u.String()

	return location, client, nil
}
