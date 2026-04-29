package client

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// GetTyped fetches one resource and materializes its typed v1alpha1 envelope.
// Empty version resolves the latest version for (kind, namespace, name).
func GetTyped[T v1alpha1.Object](
	ctx context.Context,
	c *Client,
	kind, namespace, name, version string,
	newObj func() T,
) (T, error) {
	var zero T
	if c == nil {
		return zero, fmt.Errorf("client is nil")
	}

	var (
		raw *v1alpha1.RawObject
		err error
	)
	if version == "" {
		raw, err = c.GetLatest(ctx, kind, namespace, name)
	} else {
		raw, err = c.Get(ctx, kind, namespace, name, version)
	}
	if err != nil {
		return zero, err
	}
	return v1alpha1.EnvelopeFromRaw(newObj, raw, kind)
}

// ListTyped lists resources of one kind and materializes each typed envelope.
func ListTyped[T v1alpha1.Object](
	ctx context.Context,
	c *Client,
	kind string,
	opts ListOpts,
	newObj func() T,
) ([]T, string, error) {
	if c == nil {
		return nil, "", fmt.Errorf("client is nil")
	}

	rows, nextCursor, err := c.List(ctx, kind, opts)
	if err != nil {
		return nil, "", err
	}

	out := make([]T, 0, len(rows))
	for i := range rows {
		obj, err := v1alpha1.EnvelopeFromRaw(newObj, &rows[i], kind)
		if err != nil {
			return nil, "", fmt.Errorf("decode %s row %d: %w", kind, i, err)
		}
		out = append(out, obj)
	}
	return out, nextCursor, nil
}

// ListAllTyped follows NextCursor until the full result set has been loaded.
func ListAllTyped[T v1alpha1.Object](
	ctx context.Context,
	c *Client,
	kind string,
	opts ListOpts,
	newObj func() T,
) ([]T, error) {
	var out []T
	cursor := opts.Cursor
	for {
		pageOpts := opts
		pageOpts.Cursor = cursor
		items, nextCursor, err := ListTyped(ctx, c, kind, pageOpts, newObj)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if nextCursor == "" {
			return out, nil
		}
		cursor = nextCursor
	}
}

// ListVersionsOfName returns every version of one named resource with the
// latest version first.
func ListVersionsOfName[T v1alpha1.Object](
	ctx context.Context,
	c *Client,
	kind, namespace, name string,
	newObj func() T,
) ([]T, error) {
	latest, err := GetTyped(ctx, c, kind, namespace, name, "", newObj)
	if err != nil {
		return nil, err
	}

	items, err := ListAllTyped(
		ctx,
		c,
		kind,
		ListOpts{
			Namespace: namespace,
			Limit:     200,
		},
		newObj,
	)
	if err != nil {
		return nil, err
	}

	out := make([]T, 0, len(items))
	seen := map[string]bool{}

	latestMD := latest.GetMetadata()
	out = append(out, latest)
	seen[latestMD.Version] = true

	for _, item := range items {
		md := item.GetMetadata()
		if md.Name != name || seen[md.Version] {
			continue
		}
		seen[md.Version] = true
		out = append(out, item)
	}

	return out, nil
}
