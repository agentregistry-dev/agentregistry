package gateway

// Target identifies the gateway instance that rendered config should be
// applied to or removed from. It is distinct from the pre-existing
// types.Target (Address/Hostname); the two coexist across the package
// boundary with no conflict.
type Target struct {
	Name string
	UID  string
}
