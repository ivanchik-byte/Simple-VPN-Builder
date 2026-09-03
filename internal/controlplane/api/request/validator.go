package request

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

// ValidateStruct validates a struct using go-playground/validator and returns field violations.
func ValidateStruct(s any) (bool, map[string]string) {
	err := validate.Struct(s)
	if err == nil {
		return true, nil
	}

	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		return false, map[string]string{"error": err.Error()}
	}

	errMap := make(map[string]string, len(validationErrors))
	for _, fieldErr := range validationErrors {
		field := strings.ToLower(fieldErr.Field())
		switch fieldErr.Tag() {
		case "required":
			errMap[field] = "field is required"
		case "email":
			errMap[field] = "must be a valid email address"
		case "min":
			errMap[field] = fmt.Sprintf("must be at least %s characters or value", fieldErr.Param())
		case "max":
			errMap[field] = fmt.Sprintf("must be at most %s characters or value", fieldErr.Param())
		case "oneof":
			errMap[field] = fmt.Sprintf("must be one of: %s", fieldErr.Param())
		case "uuid":
			errMap[field] = "must be a valid UUID"
		case "ip":
			errMap[field] = "must be a valid IP address"
		case "cidr":
			errMap[field] = "must be a valid CIDR notation"
		default:
			errMap[field] = fmt.Sprintf("failed validation on '%s'", fieldErr.Tag())
		}
	}

	return false, errMap
}
