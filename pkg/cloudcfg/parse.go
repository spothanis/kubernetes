package cloudcfg

import (
	"fmt"
	"reflect"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
)

var storageToType = map[string]reflect.Type{
	"pods":                   reflect.TypeOf(api.Pod{}),
	"services":               reflect.TypeOf(api.Service{}),
	"replicationControllers": reflect.TypeOf(api.ReplicationController{}),
}

// Takes input 'data' as either json or yaml, checks that it parses as the
// appropriate object type, and returns json for sending to the API or an
// error.
func ToWireFormat(data []byte, storage string) ([]byte, error) {
	prototypeType, found := storageToType[storage]
	if !found {
		return nil, fmt.Errorf("unknown storage type: %v", storage)
	}

	obj := reflect.New(prototypeType).Interface()
	err := api.DecodeInto(data, obj)
	if err != nil {
		return nil, err
	}
	return api.Encode(obj)
}
