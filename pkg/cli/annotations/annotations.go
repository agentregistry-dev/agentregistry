package annotations

// SkipDaemonAnnotation is the cobra.Command annotation key that opts a command
// out of PersistentPreRunE setup (daemon connection, API client creation, etc.).
// Add it to any command's Annotations map with the value "true" to skip.
//
//	Annotations: map[string]string{annotations.SkipDaemonAnnotation: "true"}
const SkipDaemonAnnotation = "skipDaemon"
