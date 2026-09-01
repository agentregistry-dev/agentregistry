package v1alpha1

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterRuntimeConfigValidator(t *testing.T) {
	cleanupRuntimeConfigValidator(t, "TestRegisteredRuntime")
	validatorErr := errors.New("invalid test config")
	err := RegisterRuntimeConfigValidator(
		"TestRegisteredRuntime",
		func(map[string]any) error { return validatorErr },
	)
	require.NoError(t, err)

	errs := validateRegisteredRuntimeConfig("TestRegisteredRuntime", map[string]any{})
	require.Len(t, errs, 1)
	require.ErrorIs(t, errs[0].Cause, validatorErr)
}

func TestRegisterRuntimeConfigValidatorRejectsInvalidRegistration(t *testing.T) {
	validator := func(map[string]any) error { return nil }
	tests := []struct {
		name        string
		runtimeType string
		validator   func(map[string]any) error
		wantErr     string
	}{
		{name: "empty runtime type", validator: validator, wantErr: "runtime type is required"},
		{name: "nil validator", runtimeType: "TestNilValidator", wantErr: "runtime config validator"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RegisterRuntimeConfigValidator(tt.runtimeType, tt.validator)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRegisterRuntimeConfigValidatorRejectsDuplicate(t *testing.T) {
	cleanupRuntimeConfigValidator(t, "TestDuplicateValidator")
	validator := func(map[string]any) error { return nil }
	require.NoError(t, RegisterRuntimeConfigValidator("TestDuplicateValidator", validator))

	err := RegisterRuntimeConfigValidator("TestDuplicateValidator", validator)
	require.ErrorContains(t, err, "already registered")
}

func cleanupRuntimeConfigValidator(t *testing.T, runtimeType string) {
	t.Helper()
	t.Cleanup(func() {
		runtimeConfigValidatorsMu.Lock()
		defer runtimeConfigValidatorsMu.Unlock()
		delete(runtimeConfigValidators, runtimeType)
	})
}
