package backends

import (
	"errors"
	"strings"

	"github.com/ltkh/confd/backends/etcd"
)

// The StoreClient interface is implemented by objects that can retrieve
// key/value pairs from a backend store.
type StoreClient interface {
	GetValues(keys []string) (map[string]string, error)
	WatchPrefix(prefix string, keys []string, waitIndex uint64, stopChan chan bool) (uint64, error)
}

// New is used to create a storage client based on our configuration.
func New(config Config) (StoreClient, error) {

	if config.Backend == "" {
		config.Backend = "etcd"
	}
	backendNodes := config.BackendNodes

	switch config.Backend {
		case "etcd":
			// Create the etcd client upfront and use it for the life of the process.
			// The etcdClient is an http.Client and designed to be reused.
			return etcd.NewEtcdClient(backendNodes, config.ClientCert, config.ClientKey, config.ClientCaKeys, config.ClientInsecure, config.BasicAuth, config.Username, config.Password)
	}
	return nil, errors.New("Invalid backend")
}
