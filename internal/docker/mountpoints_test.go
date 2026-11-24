package docker_test

import (
	"reflect"
	"testing"

	"oxorg/attuine/internal/docker"
)

func TestParseNestedMountpoints(t *testing.T) {
	cfg := []byte(`{"services":{
		"web":{"volumes":[
			{"type":"bind","source":"/src/web","target":"/app"},
			{"type":"volume","source":"web_modules","target":"/app/frontend/node_modules"},
			{"type":"volume","source":"web_data","target":"/data"},
			{"type":"volume","source":"web_app2","target":"/app2"}
		]},
		"api":{"volumes":[
			{"type":"bind","source":"/src/api","target":"/app/"},
			{"type":"bind","source":"/src/shared","target":"/app/vendor/shared"},
			{"type":"tmpfs","target":"/app/tmp"},
			{"type":"volume","source":"api_cache","target":"/app/vendor/shared/cache"}
		]},
		"db":{"volumes":[{"type":"volume","source":"pg","target":"/var/lib/postgresql/data"}]},
		"bare":{}
	}}`)

	got, err := docker.ParseNestedMountpoints(cfg)
	if err != nil {
		t.Fatalf("ParseNestedMountpoints() error: %v", err)
	}
	want := []docker.NestedMountpoint{
		{Service: "api", Target: "/app/tmp", HostPath: "/src/api/tmp"},
		{Service: "api", Target: "/app/vendor/shared/cache", HostPath: "/src/shared/cache"},
		{Service: "web", Target: "/app/frontend/node_modules", HostPath: "/src/web/frontend/node_modules"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseNestedMountpoints() =\n %v\nwant\n %v", got, want)
	}
}

func TestParseNestedMountpoints_InvalidJSON(t *testing.T) {
	if _, err := docker.ParseNestedMountpoints([]byte("not json")); err == nil {
		t.Error("ParseNestedMountpoints() error = nil, want an error")
	}
}
