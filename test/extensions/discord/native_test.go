package extension

import (
	"context"
	"testing"
	"time"

	e "gowarp/core/extension"
	"gowarp/core/project"
	"gowarp/core/vm"
)

func TestPresenceDoesNotNeedHostAPI(t *testing.T) {
	ext := New()
	result, err := hasPresence(&e.Context{State: ext.State})
	if err != nil || result.Value != true {
		t.Fatalf("native presence needs a host API: %+v, %v", result, err)
	}
}

func TestPresenceOwnsDispose(t *testing.T) {
	ext := New()
	dispose := ext.Events["dispose"]
	if dispose == nil {
		t.Fatal("extension must own connection disposal")
	}
	if _, err := dispose(&e.Context{State: ext.State}); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeDisposesPresenceDuringBlockedLogin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	release := make(chan struct{})
	loggedOut := make(chan struct{}, 2)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	client := &Client{manager: newManager(ctx, transport{
		login: func(string) error {
			close(entered)
			<-release
			return nil
		},
		update: func(Activity) error { return nil },
		logout: func() { loggedOut <- struct{}{} },
	})}
	ext := New()
	ext.State["client"] = client
	rt := vm.New(&project.Project{})
	if err := rt.SetExtensions(e.Bundle{func() e.Extension { return ext }}); err != nil {
		t.Fatal(err)
	}
	if err := client.Connect("123"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- rt.CloseExtensions() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime disposal waited for Discord")
	}
	if status := client.Status(); status.ApplicationID != "" || status.Ready {
		t.Fatalf("dispose retained connection state: %+v", status)
	}
	close(release)
	select {
	case <-loggedOut:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not log out after the pending login finished")
	}
}
