package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/logging"
)

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
