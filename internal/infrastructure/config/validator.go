package config

import "fmt"

type Validator interface {
	Validate() error
}

func ValidateField[T Validator](name string, v T) error {
	if err := v.Validate(); err != nil {
		return fmt.Errorf("%s, %w", name, err)
	}
	return nil
}
