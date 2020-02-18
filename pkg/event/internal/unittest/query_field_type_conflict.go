// Copyright (c) Facebook, Inc. and its affiliates.
//
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// +build go1.13

package unittest

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/unsafetools"
)

//go:linkname reflectTypelinks reflect.typelinks
func reflectTypelinks() (sections []unsafe.Pointer, offset [][]int32)

//go:linkname reflectAdd reflect.add
func reflectAdd(p unsafe.Pointer, x uintptr, whySafe string) unsafe.Pointer

func implements(queryFieldInterfaceSample interface{}) (result []reflect.Type) {
	queryFieldInterface := reflect.TypeOf(queryFieldInterfaceSample).Elem()

	sections, offsets := reflectTypelinks()
	for i, base := range sections {
		for _, offset := range offsets[i] {

			typeAddr := reflectAdd(base, uintptr(offset), "")
			typ := reflect.TypeOf(*(*interface{})(unsafe.Pointer(&typeAddr)))
			if !typ.Implements(queryFieldInterface) {
				continue
			}
			result = append(result, typ)
		}
	}
	return
}

func getZeroFieldsMap(t *testing.T, _struct reflect.Value, curPath string) map[string]bool {
	_struct = reflect.Indirect(_struct)
	structType := _struct.Type()

	result := map[string]bool{}
	for fieldIdx := 0; fieldIdx < _struct.NumField(); fieldIdx++ {
		field := _struct.Field(fieldIdx)
		fieldType := structType.Field(fieldIdx)
		fieldName := fieldType.Name

		if fieldType.Type.Kind() != reflect.Struct {
			result[curPath+fieldName] = field.IsZero()
			continue
		}
		effect := getZeroFieldsMap(t, field, curPath+fieldName+`.`)
		for fieldPath, isAffected := range effect {
			result[fieldPath] = isAffected
		}
	}
	return result
}

func fill(t *testing.T, v reflect.Value) {
	b := unsafetools.BytesOf(v.Interface())
	for idx := range b {
		b[idx] = 0xff
	}
}

func TestQueryFieldTypeConflicts(t *testing.T, queryFieldInterfaceSample interface{}) {
	fieldsAffected := map[string]uint{}
	for _, typ := range implements(queryFieldInterfaceSample) {
		queryField := reflect.New(typ.Elem())

		applyToQueryMethod := queryField.Elem().MethodByName("ApplyToQuery")
		queryType := applyToQueryMethod.Type().In(0)
		query := reflect.New(queryType.Elem())

		// Fill the `*Query` with non-zero data
		fill(t, query)

		// It will reset to zero only the field(s) if affects
		applyToQueryMethod.Call([]reflect.Value{query})

		// Find the affected field:
		effect := getZeroFieldsMap(t, query.Elem(), ``)
		for fieldName, isAffected := range effect {
			fieldsAffected[fieldName] = fieldsAffected[fieldName] // init value if required
			if isAffected {
				fieldsAffected[fieldName]++
			}
		}
	}

	for fieldName, affectedCount := range fieldsAffected {
		assert.Equal(t, uint(1), affectedCount, `field "%v" should be affected with exactly one QueryField`, fieldName)
	}
}
