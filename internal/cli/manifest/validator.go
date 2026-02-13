package manifest

// Validator validates a manifest of type T.
type Validator[T any] interface {
	Validate(manifest T) error
}
