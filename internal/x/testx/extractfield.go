package testx

import (
	"fmt"
	"reflect"
	"strings"
)

// ExtractField projects the named field out of every element of calls into a
// new slice whose element type is that field's type, returned boxed in an any.
//
// calls is a slice (or array) of structs — typically a moq "...Calls()" log
// whose element type is an anonymous struct, so ExtractField works entirely via
// reflection on an unnamed type. The returned value's concrete dynamic type is
// []FieldType (e.g. []int64), which lets it be asserted whole against a plainly
// typed expected slice:
//
//	calls := gh.VerifyInstallationAccessCalls()
//	assert.DeepEqual(t, testx.ExtractField(calls, "InstallationID"), []int64{installationID})
//
// Because the result is a concrete typed slice boxed in an any, go-cmp (via
// assert.DeepEqual) unwraps both arguments to the same []FieldType and compares
// them structurally, without the type-mismatch noise a []any or wrapper would
// produce.
//
// If the element type is a pointer to a struct ([]*T), each element is
// dereferenced before the field is read, so both value and pointer call logs
// work.
//
// ExtractField takes no testing.TB; bad input panics with a descriptive
// message: calls must be a slice/array of structs (or pointers to structs), and
// name must be a field on the element struct.
func ExtractField(calls any, name string) any {
	v := reflect.ValueOf(calls)
	if !v.IsValid() {
		panic("testx.ExtractField: calls is nil")
	}
	t := v.Type()
	if k := t.Kind(); k != reflect.Slice && k != reflect.Array {
		panic(fmt.Sprintf("testx.ExtractField: calls must be a slice or array, got %s", t))
	}

	// Derive the struct type from the container's element type (not a runtime
	// element) so an empty calls still yields a correctly typed empty slice.
	elem := t.Elem()
	structType := elem
	pointer := false
	if structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
		pointer = true
	}
	if structType.Kind() != reflect.Struct {
		panic(fmt.Sprintf("testx.ExtractField: calls element must be a struct or pointer to struct, got %s", elem))
	}

	field, ok := structType.FieldByName(name)
	if !ok {
		panic(fmt.Sprintf("testx.ExtractField: %s has no field %q (available: %s)",
			structType, name, strings.Join(fieldNames(structType), ", ")))
	}

	n := v.Len()
	out := reflect.MakeSlice(reflect.SliceOf(field.Type), n, n)
	for i := range n {
		e := v.Index(i)
		if pointer {
			e = e.Elem()
		}
		out.Index(i).Set(e.FieldByName(name))
	}
	return out.Interface()
}

// fieldNames returns the exported and unexported field names of a struct type,
// used to make the "no such field" panic actionable.
func fieldNames(t reflect.Type) []string {
	names := make([]string, t.NumField())
	for i := range names {
		names[i] = t.Field(i).Name
	}
	return names
}
