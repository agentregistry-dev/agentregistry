package kinds_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
)

type testSpec struct {
	Name string `yaml:"name"`
}

type testReq struct {
	ReqName string
	Version string
}

type testResp struct {
	ID string
}

func TestMakeApplyFunc_Success(t *testing.T) {
	var called bool
	var gotReq *testReq

	toReq := func(md kinds.Metadata, spec *testSpec) *testReq {
		return &testReq{ReqName: spec.Name, Version: md.Version}
	}
	svcApply := func(_ context.Context, req *testReq) (*testResp, error) {
		called = true
		gotReq = req
		return &testResp{ID: "ok"}, nil
	}

	fn := kinds.MakeApplyFunc("test", toReq, svcApply, (func(context.Context, string, string) (*testResp, error))(nil))
	doc := &kinds.Document{
		Kind:     "test",
		Metadata: kinds.Metadata{Name: "my-thing", Version: "1.0.0"},
		Spec:     &testSpec{Name: "hello"},
	}

	res, err := fn(context.Background(), doc, kinds.ApplyOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != kinds.StatusApplied {
		t.Fatalf("expected applied, got %s", res.Status)
	}
	if res.Kind != "test" || res.Name != "my-thing" || res.Version != "1.0.0" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !called {
		t.Fatal("svcApply was not called")
	}
	if gotReq.ReqName != "hello" || gotReq.Version != "1.0.0" {
		t.Fatalf("unexpected req: %+v", gotReq)
	}
}

func TestMakeApplyFunc_DryRun_NilGet(t *testing.T) {
	var called bool
	toReq := func(md kinds.Metadata, spec *testSpec) *testReq {
		return &testReq{}
	}
	svcApply := func(_ context.Context, req *testReq) (*testResp, error) {
		called = true
		return nil, nil
	}

	fn := kinds.MakeApplyFunc("test", toReq, svcApply, (func(context.Context, string, string) (*testResp, error))(nil))
	doc := &kinds.Document{
		Kind:     "test",
		Metadata: kinds.Metadata{Name: "n", Version: "1"},
		Spec:     &testSpec{},
	}

	res, err := fn(context.Background(), doc, kinds.ApplyOpts{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// nil svcGet → safe default: would create
	if res.Status != kinds.StatusCreated {
		t.Fatalf("expected created, got %s", res.Status)
	}
	if called {
		t.Fatal("svcApply should not be called on dry run")
	}
}

func TestMakeApplyFunc_DryRun_NotFound(t *testing.T) {
	toReq := func(md kinds.Metadata, spec *testSpec) *testReq { return &testReq{} }
	svcApply := func(_ context.Context, req *testReq) (*testResp, error) { return nil, nil }
	svcGet := func(_ context.Context, name, version string) (*testResp, error) {
		return nil, fmt.Errorf("not found")
	}

	fn := kinds.MakeApplyFunc("test", toReq, svcApply, svcGet)
	doc := &kinds.Document{
		Kind:     "test",
		Metadata: kinds.Metadata{Name: "new-thing", Version: "1"},
		Spec:     &testSpec{},
	}

	res, err := fn(context.Background(), doc, kinds.ApplyOpts{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != kinds.StatusCreated {
		t.Fatalf("expected created, got %s", res.Status)
	}
}

func TestMakeApplyFunc_DryRun_Found(t *testing.T) {
	toReq := func(md kinds.Metadata, spec *testSpec) *testReq { return &testReq{} }
	svcApply := func(_ context.Context, req *testReq) (*testResp, error) { return nil, nil }
	svcGet := func(_ context.Context, name, version string) (*testResp, error) {
		return &testResp{ID: name}, nil // resource exists
	}

	fn := kinds.MakeApplyFunc("test", toReq, svcApply, svcGet)
	doc := &kinds.Document{
		Kind:     "test",
		Metadata: kinds.Metadata{Name: "existing-thing", Version: "1"},
		Spec:     &testSpec{},
	}

	res, err := fn(context.Background(), doc, kinds.ApplyOpts{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != kinds.StatusConfigured {
		t.Fatalf("expected configured, got %s", res.Status)
	}
}

func TestMakeApplyFunc_TypeMismatch(t *testing.T) {
	toReq := func(md kinds.Metadata, spec *testSpec) *testReq {
		return &testReq{}
	}
	svcApply := func(_ context.Context, req *testReq) (*testResp, error) {
		return nil, nil
	}

	fn := kinds.MakeApplyFunc("test", toReq, svcApply, (func(context.Context, string, string) (*testResp, error))(nil))
	doc := &kinds.Document{
		Kind:     "test",
		Metadata: kinds.Metadata{Name: "n"},
		Spec:     "wrong-type", // not *testSpec
	}

	_, err := fn(context.Background(), doc, kinds.ApplyOpts{})
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}
	if got := err.Error(); got != "test: unexpected spec type string" {
		t.Fatalf("unexpected error message: %s", got)
	}
}

func TestMakeApplyFunc_ServiceError(t *testing.T) {
	toReq := func(md kinds.Metadata, spec *testSpec) *testReq {
		return &testReq{}
	}
	svcApply := func(_ context.Context, req *testReq) (*testResp, error) {
		return nil, fmt.Errorf("service unavailable")
	}

	fn := kinds.MakeApplyFunc("test", toReq, svcApply, (func(context.Context, string, string) (*testResp, error))(nil))
	doc := &kinds.Document{
		Kind:     "test",
		Metadata: kinds.Metadata{Name: "n"},
		Spec:     &testSpec{},
	}

	_, err := fn(context.Background(), doc, kinds.ApplyOpts{})
	if err == nil {
		t.Fatal("expected error from service")
	}
	if got := err.Error(); got != "service unavailable" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestMakeGetFunc(t *testing.T) {
	var latestCalled, versionCalled bool

	type resp struct{ V string }
	getLatest := func(_ context.Context, name string) (*resp, error) {
		latestCalled = true
		return &resp{V: "latest"}, nil
	}
	getVersion := func(_ context.Context, name, version string) (*resp, error) {
		versionCalled = true
		return &resp{V: version}, nil
	}

	fn := kinds.MakeGetFunc(getLatest, getVersion)

	// Empty version -> getLatest
	item, err := fn(context.Background(), "foo", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !latestCalled {
		t.Fatal("getLatest was not called")
	}
	if item.(*resp).V != "latest" {
		t.Fatalf("unexpected item: %+v", item)
	}

	// Non-empty version -> getVersion
	item, err = fn(context.Background(), "foo", "2.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !versionCalled {
		t.Fatal("getVersion was not called")
	}
	if item.(*resp).V != "2.0" {
		t.Fatalf("unexpected item: %+v", item)
	}
}

func TestMakeDeleteFunc(t *testing.T) {
	var gotName, gotVersion string
	del := func(_ context.Context, name, version string) error {
		gotName = name
		gotVersion = version
		return nil
	}

	fn := kinds.MakeDeleteFunc(del)
	if err := fn(context.Background(), "foo", "1.0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotName != "foo" || gotVersion != "1.0" {
		t.Fatalf("unexpected args: name=%q version=%q", gotName, gotVersion)
	}
}

func TestMakeInitTemplate(t *testing.T) {
	fn := kinds.MakeInitTemplate("test", testSpec{Name: "default"})

	var buf bytes.Buffer
	err := fn(context.Background(), &buf, kinds.InitParams{Name: "my-thing", Version: "1.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("kind: test")) {
		t.Fatalf("expected kind: test in output, got:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte("name: my-thing")) {
		t.Fatalf("expected name: my-thing in output, got:\n%s", out)
	}
	if !bytes.Contains([]byte(out), []byte("version: 1.0.0")) {
		t.Fatalf("expected version: 1.0.0 in output, got:\n%s", out)
	}
}

func TestAssertSpec_Success(t *testing.T) {
	doc := &kinds.Document{Spec: &testSpec{Name: "hello"}}
	spec, err := kinds.AssertSpec[testSpec]("test", doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "hello" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
}

func TestAssertSpec_TypeMismatch(t *testing.T) {
	doc := &kinds.Document{Spec: "wrong"}
	_, err := kinds.AssertSpec[testSpec]("test", doc)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAppliedResult(t *testing.T) {
	doc := &kinds.Document{
		Metadata: kinds.Metadata{Name: "n", Version: "v"},
	}
	r := kinds.AppliedResult("test", doc)
	if r.Status != kinds.StatusApplied || r.Kind != "test" || r.Name != "n" || r.Version != "v" {
		t.Fatalf("unexpected result: %+v", r)
	}
}
