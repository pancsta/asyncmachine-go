//go:build !tinygo

package utils

import (
	"fmt"
	"reflect"
)

// StructFields recursively lists fields of a struct.
func StructFields(input interface{}) ([]string, error) {
	// TODO remove?
	
	v := reflect.ValueOf(input)
	t := v.Type()

	// validate
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected a struct, got %s", t.Kind())
	}

	var names []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		val := v.Field(i) // Get the value of the fieldh

		// nested args
		if field.Type.Kind() == reflect.Pointer {
			el := field.Type.Elem()
			if el.Kind() == reflect.Struct {
				if val.IsNil() {
					// skip nil pointers
					continue
				}

				value := val.Elem().Interface()
				elNames, err := StructFields(value)
				if err != nil {
					return nil, err
				}
				names = append(names, elNames...)
			}
		} else {
			names = append(names, field.Name)
		}
	}

	return names, nil
}
