package request

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type sampleStruct struct {
	Email string `validate:"required,email"`
	Role  string `validate:"required,oneof=admin viewer"`
	Count int    `validate:"min=1,max=10"`
}

func TestValidateStruct(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		valid := sampleStruct{
			Email: "admin@example.com",
			Role:  "admin",
			Count: 5,
		}
		ok, errMap := ValidateStruct(valid)
		assert.True(t, ok)
		assert.Nil(t, errMap)
	})

	t.Run("InvalidFields", func(t *testing.T) {
		invalid := sampleStruct{
			Email: "not-an-email",
			Role:  "superuser",
			Count: 0,
		}
		ok, errMap := ValidateStruct(invalid)
		assert.False(t, ok)
		assert.Contains(t, errMap["email"], "must be a valid email address")
		assert.Contains(t, errMap["role"], "must be one of: admin viewer")
		assert.Contains(t, errMap["count"], "must be at least 1")
	})
}
