package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/logging"
)

// ConfigFileClient is the implementation of the Client interface for config files.
type ConfigFileClient struct {
	// configFile specifies the config file name to watch.
	configFile string
	// id is the name of the stunnerd instance.
	id string
	// log is a leveled logger used to report progress. Either Logger or Log must be specified.
	log logging.LeveledLogger
}

// NewConfigFileClient creates a client that load or watch STUNner configurations from a local
// file.
func NewConfigFileClient(origin, id string, logger logging.LeveledLogger) (Client, error) {
	origin = strings.TrimPrefix(origin, "file://") // returns original if there is no "file://" prefix

	return &ConfigFileClient{
		configFile: origin,
		id:         id,
		log:        logger,
	}, nil

}

// String outputs the status of the client.
func (w *ConfigFileClient) String() string {
	return fmt.Sprintf("config client using file %q", w.configFile)
}

// Load grabs a new configuration from a config file.
func (w *ConfigFileClient) Load() (*stnrv1.StunnerConfig, error) {
	b, err := os.ReadFile(w.configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %s", w.configFile, err.Error())
	}

	if len(b) == 0 {
		return nil, errFileTruncated
	}

	c, err := ParseConfig(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return c, nil
}

// Watch watches a configuration file for changes. If no file exists at the given path, it will
// periodically retry until the file appears.
func (w *ConfigFileClient) Watch(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	if w.configFile == "" {
		return errors.New("uninitialized config file path")
	}

	go func() {
		for {
			// try to watch
			if err := w.Poll(ctx, ch, suppressDelete); err != nil {
				w.log.Warnf("Error loading config file %q: %s",
					w.configFile, err.Error())
			} else {
				return
			}

			if !w.tryWatchConfig(ctx) {
				return
			}
		}
	}()

	return nil
}

// Poll watches the config file and emits new configs on the specified channel. Returns an error if
// further action is needed (tryWatchConfig is to be started) or nil on normal exit.
func (w *ConfigFileClient) Poll(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	w.log.Tracef("configWatcher")

	// create a new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close() //nolint:errcheck

	config := w.configFile
	if err := watcher.Add(config); err != nil {
		return err
	}

	// emit an initial config
	c, err := w.Load()
	if err != nil {
		return err
	}

	w.log.Debugf("Initial config file successfully loaded from %q: %s", config, c.String())

	ch <- c

	// save deepcopy so that we can filter repeated events
	prev := stnrv1.StunnerConfig{}
	c.DeepCopyInto(&prev)

	for {
		select {
		case <-ctx.Done():
			return nil

		case e, ok := <-watcher.Events:
			if !ok {
				return errors.New("config watcher received invalid event")
			}

			w.log.Debugf("Received watch event: %s", e.String())

			if e.Has(fsnotify.Remove) {
				if err := watcher.Remove(config); err != nil {
					w.log.Debugf("Could not remove config file %q watcher: %s",
						config, err.Error())
				}

				return fmt.Errorf("config file deleted %q, disabling watcher", e.Op.String())
			}

			if !e.Has(fsnotify.Write) {
				w.log.Debugf("Unhandled notify op on config file %q (ignoring): %s",
					e.Name, e.Op.String())
				continue
			}

			w.log.Debugf("Loading configuration file: %s", config)
			c, err := w.Load()
			if err != nil {
				if errors.Is(err, errFileTruncated) {
					w.log.Debugf("Ignoring: %s", err.Error())
					continue
				}
				return err
			}

			// suppress repeated events
			if c.DeepEqual(&prev) {
				w.log.Debug("Ignoring recurrent notify event for the same config file")
				continue
			}

			if suppressDelete && IsConfigDeleted(c) {
				w.log.Info("Ignoring deleted configuration")
				continue
			}

			w.log.Debugf("Config file successfully loaded from %q: %s", config, c.String())

			ch <- c

			// save deepcopy so that we can filter repeated events
			c.DeepCopyInto(&prev)

		case err, ok := <-watcher.Errors:
			if !ok {
				return errors.New("config watcher error handler received invalid error")
			}

			if err := watcher.Remove(config); err != nil {
				w.log.Debugf("Could not remove config file %q watcher: %s",
					config, err.Error())
			}

			return fmt.Errorf("config watcher error, deactivating: %w", err)
		}
	}
}

// tryWatchConfig runs a timer to look for the config file at the given path and returns it
// immediately once found. Returns true if further action is needed (configWatcher has to be
// started) or false on normal exit.
func (w *ConfigFileClient) tryWatchConfig(ctx context.Context) bool {
	w.log.Tracef("tryWatchConfig")
	config := w.configFile

	ticker := time.NewTicker(RetryPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false

		case <-ticker.C:
			w.log.Debugf("Trying to read config file %q from periodic timer",
				config)

			// check if config file exists and it is readable
			if _, err := os.Stat(config); errors.Is(err, os.ErrNotExist) {
				w.log.Debugf("Config file %q does not exist", config)

				// report status in every 10th second
				if time.Now().Second()%10 == 0 {
					w.log.Warnf("Waiting for config file %q", config)
				}

				continue
			}

			return true
		}
	}
}
