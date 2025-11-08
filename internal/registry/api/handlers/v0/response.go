package v0

// Response is a generic wrapper for Huma responses
// Usage: Response[HealthBody] instead of HealthOutput
type Response[T any] struct {
	Body T
}

// EmptyResponse represents a simple success response with a message
type EmptyResponse struct {
	Message string `json:"message" doc:"Success message" example:"Operation completed successfully"`
}

// Example usage:
// Instead of:
//   type HealthOutput struct {
//       Body HealthBody
//   }
//
// You could use:
//   type HealthOutput = Response[HealthBody]
//
// Or directly in the handler:
//   func(...) (*Response[HealthBody], error) {
//       return &Response[HealthBody]{
//           Body: HealthBody{...},
//       }, nil
//   }
