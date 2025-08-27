package openapi

import (
	"errors"
	"regexp"
)

var kubernetesNameValidationRegex = regexp.MustCompile("^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$")

type KubernetesName struct {
	Value string
}

func (n *KubernetesName) UnmarshalText(text []byte) error {
	if !kubernetesNameValidationRegex.Match(text) {
		return errors.New("invalid name: must consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric character")
	}

	*n = KubernetesName{
		Value: string(text),
	}

	return nil
}
