package assemble

import (
	"errors"
	"fmt"
	"reflect"
)

var (
	ErrBindingNotFound = errors.New("binding not found")
	ErrInvokeFail      = errors.New("invoke failed")
)

type BadCastError struct {
	Key  Key
	From reflect.Type
	To   reflect.Type
}

func (e BadCastError) Error() string {
	return fmt.Sprintf("bad cast %v -> %v for %v", e.From, e.To, e.Key)
}

type NotFoundError struct {
	Type reflect.Type
	Name string
}

func (e NotFoundError) Error() string {
	if e.Name != "" {
		return fmt.Sprintf("%v (name=%q): %v", e.Type, e.Name, ErrBindingNotFound)
	}
	return fmt.Sprintf("%v: %v", e.Type, ErrBindingNotFound)
}

type BindError struct {
	From reflect.Type
	To   reflect.Type
	Why  string
}

func (e BindError) Error() string {
	return fmt.Sprintf("bind %v -> %v: %s", e.From, e.To, e.Why)
}
