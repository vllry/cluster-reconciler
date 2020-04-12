package parse

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/streaming"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"reflect"
)

func ParseYaml(validYaml string) ([]unstructured.Unstructured, error) {
	yamlDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	// Parse the objects from the yaml.
	var objects []unstructured.Unstructured
	reader := json.YAMLFramer.NewFrameReader(ioutil.NopCloser(bytes.NewReader([]byte(validYaml))))
	d := streaming.NewDecoder(reader, yamlDecoder)
	for {
		obj, _, err := d.Decode(nil, nil)
		if err != nil {
			if err == io.EOF {
				break
			}
			return []unstructured.Unstructured{}, err
		}

		// Convert the Object to Unstructured.
		switch t := obj.(type) {
		case *unstructured.Unstructured:
			objects = append(objects, *t)
		default:
			return nil, errors.New(fmt.Sprintf("failed to convert object, unexpected type %s", reflect.TypeOf(obj)))
		}
	}

	return objects, nil
}