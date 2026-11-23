package extension

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for Discord worker")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSlowLoginDoesNotBlockAndKeepsOnlyLatestActivity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan string, 1)
	release := make(chan struct{})
	sent := make(chan Activity, 4)
	m := newManager(ctx, transport{
		login: func(id string) error {
			entered <- id
			<-release
			return nil
		},
		update: func(activity Activity) error {
			sent <- activity
			return nil
		},
		logout: func() {},
	})
	c := &Client{manager: m}
	finished := make(chan error, 1)
	go func() { finished <- c.Connect("123456789012345678") }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Connect blocked on Discord")
	}
	select {
	case id := <-entered:
		if id != "123456789012345678" {
			t.Fatalf("login ID = %q, want %q", id, "123456789012345678")
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not connect")
	}
	for i := 0; i < 2000; i++ {
		if err := c.Update(Activity{Details: "old", PartySize: i, PartyMax: 2000}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Update(Activity{Details: "latest"}); err != nil {
		t.Fatal(err)
	}
	if c.Status().Ready {
		t.Fatal("reported ready before handshake completed")
	}
	close(release)
	select {
	case a := <-sent:
		if a.Details != "latest" {
			t.Fatalf("sent stale activity: %+v", a)
		}
	case <-time.After(time.Second):
		t.Fatal("latest activity was not sent")
	}
	waitFor(t, func() bool { return c.Status().Ready })
	for i := 0; i < 100; i++ {
		if err := c.Update(Activity{Details: "latest"}); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case a := <-sent:
		t.Fatalf("duplicate activity sent: %+v", a)
	case <-time.After(30 * time.Millisecond):
	}
	c.Close()
}

func TestDisconnectDuringLoginCannotRestoreReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	loggedOut := make(chan struct{}, 1)
	m := newManager(ctx, transport{
		login: func(string) error {
			entered <- struct{}{}
			<-release
			return nil
		},
		update: func(Activity) error {
			t.Error("sent after disconnect")
			return nil
		},
		logout: func() {
			select {
			case loggedOut <- struct{}{}:
			default:
			}
		},
	})
	c := &Client{manager: m}
	if err := c.Connect("123"); err != nil {
		t.Fatal(err)
	}
	<-entered
	c.Close()
	if c.Status().Ready || c.Status().ApplicationID != "" {
		t.Fatal(c.Status())
	}
	close(release)
	select {
	case <-loggedOut:
	case <-time.After(time.Second):
		t.Fatal("stale connection was not closed")
	}
	if c.Status().Ready {
		t.Fatal("stale login restored ready")
	}
}

func TestOfflineDiscordReportsErrorWithoutFailingConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newManager(ctx, transport{
		login:  func(string) error { return errors.New("Discord is closed") },
		update: func(Activity) error { return nil }, logout: func() {},
	})
	c := &Client{manager: m}
	if err := c.Connect("123"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return c.Status().Error != "" })
	if c.Status().Ready {
		t.Fatal("offline client is ready")
	}
	c.Close()
}

func TestActivityValidationAndMapping(t *testing.T) {
	a := Activity{Details: "Battle", LargeImageKey: "sans", StartTimestamp: 1700000000, PartySize: 1, PartyMax: 4}
	if err := a.validate(); err != nil {
		t.Fatal(err)
	}
	native := a.native()
	if native.LargeImage != "sans" || native.Timestamps.Start.Unix() != 1700000000 || native.Party.MaxPlayers != 4 {
		t.Fatalf("incorrect library mapping: %+v", native)
	}
	if native.Instance == nil || *native.Instance {
		t.Fatal("presence should not advertise an instanced game")
	}

	for _, bad := range []Activity{{PartySize: 5, PartyMax: 4}, {StartTimestamp: -1}, {Details: strings.Repeat("x", 129)}} {
		if err := bad.validate(); err == nil {
			t.Fatalf("accepted invalid activity: %+v", bad)
		}
	}
}

func TestBlockedUpdateDoesNotBlockClearCloseOrNextProject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	logins := make(chan string, 4)
	sent := make(chan string, 4)
	m := newManager(ctx, transport{
		login: func(id string) error { logins <- id; return nil },
		update: func(a Activity) error {
			if a.Details == "first" {
				entered <- struct{}{}
				<-release
			}
			sent <- a.Details
			return nil
		},
		logout: func() {},
	})
	first := &Client{manager: m}
	second := &Client{manager: m}
	if err := first.Connect("123"); err != nil {
		t.Fatal(err)
	}
	if err := second.Connect("456"); err == nil {
		t.Fatal("two projects acquired the global client")
	}
	if err := first.Update(Activity{Details: "first"}); err != nil {
		t.Fatal(err)
	}
	<-entered
	returned := make(chan error, 1)
	go func() {
		first.Clear()
		first.Close()
		err := second.Connect("456")
		if err == nil {
			err = second.Update(Activity{Details: "second"})
		}
		returned <- err
	}()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocks waited for the outstanding update")
	}
	once.Do(func() { close(release) })
	waitFor(t, func() bool { return second.Status().Ready })
	if first.Status().Ready {
		t.Fatal("old update revived a closed client")
	}
	if id := <-logins; id != "123" {
		t.Fatal(id)
	}
	if id := <-logins; id != "456" {
		t.Fatal(id)
	}
	if text := <-sent; text != "first" {
		t.Fatal(text)
	}
	select {
	case text := <-sent:
		if text != "second" {
			t.Fatal(text)
		}
	case <-time.After(time.Second):
		t.Fatal("next project did not publish")
	}
	second.Close()
}

func TestClearReopensWithoutPublishingOldActivity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logins := make(chan struct{}, 4)
	updates := make(chan Activity, 4)
	logouts := make(chan struct{}, 4)
	m := newManager(ctx, transport{
		login:  func(string) error { logins <- struct{}{}; return nil },
		update: func(a Activity) error { updates <- a; return nil },
		logout: func() { logouts <- struct{}{} },
	})
	c := &Client{manager: m}
	if err := c.Connect("123"); err != nil {
		t.Fatal(err)
	}
	if err := c.Update(Activity{Details: "Battle"}); err != nil {
		t.Fatal(err)
	}
	<-updates
	<-logins
	c.Clear()
	select {
	case <-logouts:
	case <-time.After(time.Second):
		t.Fatal("clear did not remove presence")
	}
	select {
	case <-logins:
	case <-time.After(time.Second):
		t.Fatal("clear did not keep connection available")
	}
	select {
	case a := <-updates:
		t.Fatalf("clear republished old activity: %+v", a)
	case <-time.After(30 * time.Millisecond):
	}
	c.Close()
}

func TestOfflineRetryIsNotTriggeredByEveryUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	attempts := make(chan struct{}, 100)
	m := newManager(ctx, transport{
		login:  func(string) error { attempts <- struct{}{}; return errors.New("offline") },
		update: func(Activity) error { return nil }, logout: func() {},
	})
	c := &Client{manager: m}
	if err := c.Connect("123"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return c.Status().Error != "" })
	<-attempts
	for i := 0; i < 100; i++ {
		_ = c.Update(Activity{PartySize: i, PartyMax: 100})
	}
	select {
	case <-attempts:
		t.Fatal("updates bypassed retry delay")
	case <-time.After(50 * time.Millisecond):
	}
	c.Close()
}
