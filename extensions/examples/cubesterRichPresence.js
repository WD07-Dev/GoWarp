// Name: Rich Presence
// ID: cubesterRichPresence
// Description: Discord Rich Presence with image assets and native GoWarp support.
// Based on Rich Presence by CubesterYT <https://scratch.mit.edu/users/CubesterYT/>
// GoWarp adaptation: native Go implementation and nonblocking RPC controls.

/*
@gowarp-config {
    require github.com/KidiXDev/rich-go v0.0.0-20260519131909-c4659679e3fa
}

@gowarp-go {
// Package extension implements the cubesterRichPresence blocks for GoWarp.
package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	rich "github.com/KidiXDev/rich-go/client"

	e "gowarp/core/extension"
)

func New() e.Extension {
	result := e.Extension{
		ID: "cubesterRichPresence",
		State: map[string]any{
			"data":     map[string]any{},
			"clientID": "",
			"error":    "",
			"client":   newClient(),
		},
		Events: map[string]e.Handler{"dispose": disconnect},
		Blocks: []e.Block{
			{Opcode: "cubesterRichPresence_hasPresence", Kind: "boolean", Handler: hasPresence},
			{Opcode: "cubesterRichPresence_setClientID", Kind: "command", Inputs: []string{"ID"}, Handler: setClientID},
			{Opcode: "cubesterRichPresence_clientID", Kind: "reporter", Handler: clientID},
			{Opcode: "cubesterRichPresence_setData", Kind: "command", Inputs: []string{"INPUT"}, Handler: setData},
			{Opcode: "cubesterRichPresence_setImage", Kind: "command", Inputs: []string{"SIZE", "IMAGE", "TEXT"}, Handler: setImage},
			{Opcode: "cubesterRichPresence_setTimestamp", Kind: "command", Handler: setTimestamp},
			{Opcode: "cubesterRichPresence_presenceData", Kind: "reporter", Handler: presenceData},
			{Opcode: "cubesterRichPresence_presenceReady", Kind: "boolean", Handler: presenceReady},
			{Opcode: "cubesterRichPresence_updatePresence", Kind: "command", Handler: updatePresence},
			{Opcode: "cubesterRichPresence_clearPresence", Kind: "command", Handler: clearPresence},
			{Opcode: "cubesterRichPresence_disconnect", Kind: "command", Handler: disconnect},
			{Opcode: "cubesterRichPresence_lastError", Kind: "reporter", Handler: lastError},
		},
	}
	for _, name := range []string{"DATA", "SIZE", "TOGGLE"} {
		result.Blocks = append(result.Blocks, e.Block{
			Opcode: "cubesterRichPresence_menu_" + name,
			Kind:   "menu",
			Handler: func(c *e.Context) (e.Result, error) {
				return e.Result{Value: c.Field(name)}, nil
			},
		})
	}
	return result
}

// Discord availability must not stop a project's scripts.
func rememberError(c *e.Context, err error) {
	c.State["error"] = ""
	if err != nil {
		c.State["error"] = err.Error()
	}
}

func hasPresence(c *e.Context) (e.Result, error) {
	return e.Result{Value: true}, nil
}

func setClientID(c *e.Context) (e.Result, error) {
	id := c.String("ID")
	err := c.State["client"].(*Client).Connect(id)
	rememberError(c, err)
	if err == nil {
		c.State["clientID"] = id
	}
	return e.Result{}, nil
}

func clientID(c *e.Context) (e.Result, error) {
	return e.Result{Value: c.State["clientID"]}, nil
}

var presenceFields = map[string]string{
	"details":          "details",
	"state":            "state",
	"large image key":  "largeImageKey",
	"large image text": "largeImageText",
	"small image key":  "smallImageKey",
	"small image text": "smallImageText",
	"party id":         "partyId",
	"party size":       "partySize",
	"party max":        "partyMax",
}

func setData(c *e.Context) (e.Result, error) {
	name := c.Field("DATA")
	if name == "JSON" {
		var data map[string]any
		err := json.Unmarshal([]byte(c.String("INPUT")), &data)
		if err == nil && data == nil {
			err = fmt.Errorf("rich presence data must be a JSON object")
		}
		rememberError(c, err)
		if err != nil {
			return e.Result{}, nil
		}
		// Instance is always false in the activity sent to Discord.
		delete(data, "instance")
		c.State["data"] = data
		return e.Result{}, nil
	}

	key, ok := presenceFields[name]
	if !ok {
		return e.Result{}, nil
	}
	data := c.State["data"].(map[string]any)
	if name == "party size" || name == "party max" {
		data[key] = c.Number("INPUT")
	} else {
		data[key] = c.String("INPUT")
	}
	rememberError(c, nil)
	return e.Result{}, nil
}

func setImage(c *e.Context) (e.Result, error) {
	size := c.String("SIZE")
	if size != "large" && size != "small" {
		return e.Result{}, nil
	}
	data := c.State["data"].(map[string]any)
	data[size+"ImageKey"] = c.String("IMAGE")
	data[size+"ImageText"] = c.String("TEXT")
	rememberError(c, nil)
	return e.Result{}, nil
}

func setTimestamp(c *e.Context) (e.Result, error) {
	data := c.State["data"].(map[string]any)
	switch c.Field("TOGGLE") {
	case "on":
		data["startTimestamp"] = time.Now().Unix()
	case "off":
		delete(data, "startTimestamp")
	}
	return e.Result{}, nil
}

func presenceData(c *e.Context) (e.Result, error) {
	data, err := json.Marshal(c.State["data"])
	if err != nil {
		rememberError(c, err)
		return e.Result{Value: "{}"}, nil
	}
	return e.Result{Value: string(data)}, nil
}

func presenceReady(c *e.Context) (e.Result, error) {
	status := c.State["client"].(*Client).Status()
	return e.Result{Value: status.Ready}, nil
}

func updatePresence(c *e.Context) (e.Result, error) {
	activity, err := activityFromData(c.State["data"].(map[string]any))
	if err == nil {
		err = c.State["client"].(*Client).Update(activity)
	}
	rememberError(c, err)
	return e.Result{}, nil
}

func clearPresence(c *e.Context) (e.Result, error) {
	c.State["client"].(*Client).Clear()
	rememberError(c, nil)
	return e.Result{}, nil
}

func disconnect(c *e.Context) (e.Result, error) {
	c.State["client"].(*Client).Close()
	rememberError(c, nil)
	c.State["clientID"] = ""
	return e.Result{}, nil
}

func lastError(c *e.Context) (e.Result, error) {
	if message, _ := c.State["error"].(string); message != "" {
		return e.Result{Value: message}, nil
	}
	status := c.State["client"].(*Client).Status()
	return e.Result{Value: status.Error}, nil
}

func activityFromData(values map[string]any) (Activity, error) {
	data, err := json.Marshal(values)
	if err != nil {
		return Activity{}, fmt.Errorf("discord activity: %w", err)
	}
	if len(data) > 16*1024 {
		return Activity{}, fmt.Errorf("discord activity exceeds 16 KiB")
	}
	var activity Activity
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&activity); err != nil {
		return Activity{}, fmt.Errorf("discord activity: %w", err)
	}
	return activity, nil
}

// Activity is the extension-facing presence data. Timestamps are Unix seconds.
// Empty fields are omitted; each update replaces the entire activity.
type Activity struct {
	Details        string `json:"details,omitempty"`
	State          string `json:"state,omitempty"`
	LargeImageKey  string `json:"largeImageKey,omitempty"`
	LargeImageText string `json:"largeImageText,omitempty"`
	SmallImageKey  string `json:"smallImageKey,omitempty"`
	SmallImageText string `json:"smallImageText,omitempty"`
	StartTimestamp int64  `json:"startTimestamp,omitempty"`
	EndTimestamp   int64  `json:"endTimestamp,omitempty"`
	PartyID        string `json:"partyId,omitempty"`
	PartySize      int    `json:"partySize,omitempty"`
	PartyMax       int    `json:"partyMax,omitempty"`
}

func (a Activity) validate() error {
	for _, field := range []struct{ name, value string }{
		{"details", a.Details}, {"state", a.State},
		{"largeImageText", a.LargeImageText}, {"smallImageText", a.SmallImageText},
	} {
		if field.value != "" && utf8.RuneCountInString(field.value) < 2 {
			return fmt.Errorf("discord: %s must be empty or contain at least 2 characters", field.name)
		}
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{
		{"details", a.Details, 128}, {"state", a.State, 128},
		{"largeImageKey", a.LargeImageKey, 2048}, {"smallImageKey", a.SmallImageKey, 2048},
		{"largeImageText", a.LargeImageText, 128}, {"smallImageText", a.SmallImageText, 128},
		{"partyId", a.PartyID, 128},
	} {
		if !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) > field.limit {
			return fmt.Errorf("discord: %s must be valid text of at most %d characters", field.name, field.limit)
		}
	}
	if a.StartTimestamp < 0 || a.EndTimestamp < 0 || (a.EndTimestamp != 0 && a.EndTimestamp < a.StartTimestamp) {
		return fmt.Errorf("discord: invalid timestamps (use Unix seconds)")
	}
	if a.PartySize < 0 || a.PartyMax < 0 || a.PartySize > a.PartyMax {
		return fmt.Errorf("discord: party size must be between zero and party max")
	}
	return nil
}

func (a Activity) native() rich.Activity {
	instance := false
	result := rich.Activity{
		Details: a.Details, State: a.State,
		LargeImage: a.LargeImageKey, LargeText: a.LargeImageText,
		SmallImage: a.SmallImageKey, SmallText: a.SmallImageText,
		Instance: &instance,
	}
	if a.StartTimestamp != 0 || a.EndTimestamp != 0 {
		result.Timestamps = &rich.Timestamps{}
		if a.StartTimestamp != 0 {
			start := time.Unix(a.StartTimestamp, 0)
			result.Timestamps.Start = &start
		}
		if a.EndTimestamp != 0 {
			end := time.Unix(a.EndTimestamp, 0)
			result.Timestamps.End = &end
		}
	}
	if a.PartyMax > 0 {
		result.Party = &rich.Party{ID: a.PartyID, Players: a.PartySize, MaxPlayers: a.PartyMax}
	}
	return result
}

// Status is a snapshot of the worker's last result, not a synchronous query.
type Status struct {
	ApplicationID string
	Ready         bool
	Error         string
}

type Client struct {
	manager *manager
	status  Status // protected by manager.mu
}

type transport struct {
	login  func(string) error
	update func(Activity) error
	logout func()
}

type request struct {
	owner      *Client
	id         string
	activity   *Activity // immutable copy, nil means no presence
	revision   uint64
	generation uint64
}

type manager struct {
	mu        sync.Mutex
	once      sync.Once
	desired   request
	wake      chan struct{}
	ctx       context.Context
	transport transport
}

// One worker owns rich-go's global socket and retains only the latest request.
var shared = newManager(context.Background(), transport{
	login: rich.Login,
	update: func(activity Activity) error {
		return rich.SetActivity(activity.native())
	},
	logout: rich.Logout,
})

func newManager(ctx context.Context, backend transport) *manager {
	return &manager{ctx: ctx, transport: backend, wake: make(chan struct{}, 1)}
}

// newClient does not connect or start a worker. Connect must be explicitly requested.
func newClient() *Client {
	return &Client{manager: shared}
}

func (c *Client) Connect(id string) error {
	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil || value == 0 {
		return fmt.Errorf("discord: application ID must be a nonzero decimal string")
	}
	for _, digit := range id {
		if digit < '0' || digit > '9' {
			return fmt.Errorf("discord: application ID must contain only digits")
		}
	}
	m := c.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.desired.owner != nil && m.desired.owner != c {
		return fmt.Errorf("discord: another project owns the connection")
	}
	if m.desired.owner == c && m.desired.id == id {
		return nil
	}
	m.desired = request{owner: c, id: id, revision: 1, generation: m.desired.generation + 1}
	c.status = Status{ApplicationID: id}
	m.once.Do(func() { go m.run() })
	m.notify()
	return nil
}

func (c *Client) Update(activity Activity) error {
	for _, text := range []*string{&activity.Details, &activity.State, &activity.LargeImageText, &activity.SmallImageText} {
		if strings.TrimSpace(*text) == "" {
			*text = ""
		}
	}
	if err := activity.validate(); err != nil {
		return err
	}
	m := c.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.desired.owner != c {
		return fmt.Errorf("discord: connect with an application ID before updating presence")
	}
	if old := m.desired.activity; old != nil && *old == activity {
		return nil
	}
	m.desired.activity = &activity
	m.desired.revision++
	m.notify()
	return nil
}

func (c *Client) Clear() {
	m := c.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.desired.owner == c && m.desired.activity != nil {
		m.desired.activity = nil
		m.desired.revision++
		m.notify()
	}
}

// Close returns immediately; the worker logs out after its current call.
func (c *Client) Close() {
	m := c.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.desired.owner == c {
		m.desired = request{generation: m.desired.generation + 1}
		m.notify()
	}
	c.status = Status{}
}

func (c *Client) Status() Status {
	c.manager.mu.Lock()
	defer c.manager.mu.Unlock()
	return c.status
}

func (m *manager) notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *manager) snapshot() request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.desired
}

func sameConnection(a, b request) bool {
	return a.owner == b.owner && a.generation == b.generation
}

func (m *manager) report(r request, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !sameConnection(r, m.desired) {
		return
	}
	r.owner.status.Ready = err == nil
	r.owner.status.Error = ""
	if err != nil {
		r.owner.status.Error = err.Error()
	}
}

func (m *manager) run() {
	var connected request
	var sent, generation uint64
	var retryAt, sendAt, refreshAt time.Time
	defer m.transport.logout()
	for {
		if m.ctx.Err() != nil {
			return
		}
		desired := m.snapshot()
		if !sameConnection(connected, desired) {
			if connected.owner != nil {
				m.transport.logout()
			}
			connected = request{}
			sent = 0
		}
		// Changing owner/ID should not inherit a failed connection's delay.
		if desired.generation != generation {
			generation = desired.generation
			retryAt = time.Time{}
		}
		if desired.owner != nil && connected.owner == nil && !time.Now().Before(retryAt) {
			err := m.transport.login(desired.id)
			m.report(desired, err)
			if err != nil {
				m.transport.logout()
				retryAt = time.Now().Add(5 * time.Second)
			} else {
				connected = desired
				sendAt = time.Time{}
				if desired.activity == nil {
					sent = desired.revision
				}
			}
		}
		if connected.owner != nil {
			desired = m.snapshot()
			if !sameConnection(connected, desired) {
				continue
			}
			if desired.activity == nil && sent != desired.revision {
				// Upstream exposes logout rather than ClearActivity. Reopen the
				// connection without publishing anything to remove the activity.
				m.transport.logout()
				connected = request{}
				retryAt = time.Time{}
				continue
			}
			if desired.activity != nil && !time.Now().Before(sendAt) &&
				(sent != desired.revision || !time.Now().Before(refreshAt)) {
				err := m.transport.update(*desired.activity)
				m.report(desired, err)
				if err != nil {
					m.transport.logout()
					connected = request{}
					retryAt = time.Now().Add(5 * time.Second)
				} else {
					sent = desired.revision
					sendAt = time.Now().Add(5 * time.Second)
					refreshAt = time.Now().Add(15 * time.Second)
				}
			}
		}
		var timer *time.Timer
		var tick <-chan time.Time
		if desired.owner != nil {
			timer = time.NewTimer(time.Second)
			tick = timer.C
		}
		select {
		case <-m.ctx.Done():
		case <-m.wake:
		case <-tick:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}
}
*/
(function (Scratch) {
  "use strict";

  class RichPresence {
    getInfo() {
      return {
        id: "cubesterRichPresence",
        name: "Rich Presence",
        color1: "#4D5057",
        docsURI: "https://extensions.turbowarp.org/CubesterYT/RichPresence",

        blocks: [
          {
            opcode: "hasPresence",
            text: Scratch.translate("has rich presence support?"),
            blockType: Scratch.BlockType.BOOLEAN,
          },
          {
            opcode: "setClientID",
            text: Scratch.translate("set client id [ID] and log in"),
            blockType: Scratch.BlockType.COMMAND,
            arguments: {
              ID: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "",
              },
            },
          },
          {
            opcode: "clientID",
            text: Scratch.translate("client id"),
            blockType: Scratch.BlockType.REPORTER,
          },

          "---",

          {
            opcode: "setData",
            text: Scratch.translate("set [DATA] to [INPUT]"),
            blockType: Scratch.BlockType.COMMAND,
            arguments: {
              DATA: {
                type: Scratch.ArgumentType.STRING,
                menu: "DATA",
              },
              INPUT: {
                type: Scratch.ArgumentType.STRING,
              },
            },
          },
          {
            opcode: "setImage",
            text: Scratch.translate("set [SIZE] image [IMAGE] hover text [TEXT]"),
            blockType: Scratch.BlockType.COMMAND,
            arguments: {
              SIZE: { type: Scratch.ArgumentType.STRING, menu: "SIZE" },
              IMAGE: { type: Scratch.ArgumentType.STRING, defaultValue: "game_logo" },
              TEXT: { type: Scratch.ArgumentType.STRING, defaultValue: "My game" },
            },
          },
          {
            opcode: "setTimestamp",
            text: "turn timestamp [TOGGLE]",
            blockType: Scratch.BlockType.COMMAND,
            arguments: {
              TOGGLE: {
                type: Scratch.ArgumentType.STRING,
                menu: "TOGGLE",
              },
            },
          },
          {
            opcode: "presenceData",
            text: Scratch.translate("rich presence data"),
            blockType: Scratch.BlockType.REPORTER,
          },

          "---",

          {
            opcode: "presenceReady",
            text: Scratch.translate("is rich presence ready?"),
            blockType: Scratch.BlockType.BOOLEAN,
          },
          {
            opcode: "updatePresence",
            text: Scratch.translate("update rich presence"),
            blockType: Scratch.BlockType.COMMAND,
          },
          {
            opcode: "clearPresence",
            text: Scratch.translate("clear rich presence"),
            blockType: Scratch.BlockType.COMMAND,
          },
          {
            opcode: "disconnect",
            text: Scratch.translate("disconnect rich presence"),
            blockType: Scratch.BlockType.COMMAND,
          },
          {
            opcode: "lastError",
            text: Scratch.translate("rich presence last error"),
            blockType: Scratch.BlockType.REPORTER,
          },
        ],
        menus: {
          SIZE: {
            acceptReporters: true,
            items: [
              { text: Scratch.translate("large"), value: "large" },
              { text: Scratch.translate("small"), value: "small" },
            ],
          },
          DATA: {
            acceptReporters: false,
            items: [
              { text: Scratch.translate("details"), value: "details" },
              { text: Scratch.translate("state"), value: "state" },
              {
                text: Scratch.translate("large image key or URL"),
                value: "large image key",
              },
              {
                text: Scratch.translate("large image text"),
                value: "large image text",
              },
              {
                text: Scratch.translate("small image key or URL"),
                value: "small image key",
              },
              {
                text: Scratch.translate("small image text"),
                value: "small image text",
              },
              { text: Scratch.translate("party id"), value: "party id" },
              { text: Scratch.translate("party size"), value: "party size" },
              { text: Scratch.translate("party max"), value: "party max" },
              "JSON",
            ],
          },
          TOGGLE: {
            acceptReporters: false,
            items: ["on", "off"],
          },
        },
      };
    }

    constructor() {
      this.client = null;
      this.applicationID = "";
      this.data = {};
      this.ready = false;
      this.error = "";
    }

    hasPresence() {
      return typeof RPC !== "undefined" && typeof RPC.Client === "function";
    }

    setClientID(args) {
      if (!this.hasPresence()) {
        this.error = "Rich Presence requires GoWarp or a host with Discord RPC support";
        return;
      }
      const id = Scratch.Cast.toString(args.ID);
      if (!/^[0-9]+$/.test(id) || BigInt(id) === 0n || BigInt(id) > 18446744073709551615n) {
        this.error = "Application ID must be a nonzero decimal string";
        return;
      }
      if (this.client && id === this.applicationID) return;
      this.disconnect();
      try {
        const client = new RPC.Client({ transport: "ipc" });
        this.client = client;
        this.applicationID = id;
        this.error = "";
        client.on("ready", () => {
          if (this.client === client) this.ready = true;
        });
        client.on("disconnected", () => {
          if (this.client === client) this.ready = false;
        });
        client.on("error", error => {
          if (this.client === client) {
            this.ready = false;
            this.error = String(error.message || error);
          }
        });
        // discord-rpc spells this option clientId. Keep it a string.
        this.runRPC(client => client.login({ clientId: id }));
      } catch (error) {
        this.client = null;
        this.applicationID = "";
        this.error = String(error.message || error);
      }
    }

    clientID() {
      return this.applicationID;
    }

    setData(args) {
      const fields = {
        details: "details", state: "state",
        "large image key": "largeImageKey", "large image text": "largeImageText",
        "small image key": "smallImageKey", "small image text": "smallImageText",
        "party id": "partyId", "party size": "partySize", "party max": "partyMax",
      };
      const name = Scratch.Cast.toString(args.DATA);
      if (name === "JSON") {
        try {
          const value = JSON.parse(Scratch.Cast.toString(args.INPUT));
          if (!value || typeof value !== "object" || Array.isArray(value)) {
            throw new Error("Rich presence data must be a JSON object");
          }
          delete value.instance;
          this.data = value;
          this.error = "";
        } catch (error) {
          this.error = String(error.message || error);
        }
      } else if (Object.prototype.hasOwnProperty.call(fields, name)) {
        this.data[fields[name]] = name === "party size" || name === "party max"
          ? Scratch.Cast.toNumber(args.INPUT) : Scratch.Cast.toString(args.INPUT);
        this.error = "";
      }
    }

    setImage(args) {
      const size = Scratch.Cast.toString(args.SIZE);
      if (size !== "large" && size !== "small") return;
      this.data[size + "ImageKey"] = Scratch.Cast.toString(args.IMAGE);
      this.data[size + "ImageText"] = Scratch.Cast.toString(args.TEXT);
      this.error = "";
    }

    setTimestamp(args) {
      switch (Scratch.Cast.toString(args.TOGGLE)) {
        case "on":
          this.data.startTimestamp = Math.floor(Date.now() / 1000);
          break;
        case "off":
          delete this.data.startTimestamp;
          break;
      }
    }

    presenceData() {
      return JSON.stringify(this.data);
    }

    presenceReady() {
      return this.ready;
    }

    runRPC(operation) {
      const client = this.client;
      if (!client) {
        this.error = "Connect with an application ID before updating presence";
        return;
      }
      this.error = "";
      // Do not return this promise: Scratch blocks must not wait for IPC.
      Promise.resolve().then(() => {
        if (this.client === client) return operation(client);
      }).catch(error => {
        if (this.client === client) this.error = String(error.message || error);
      });
    }

    updatePresence() {
      const activity = { ...this.data, instance: false };
      for (const field of ["details", "state", "largeImageText", "smallImageText"]) {
        if (typeof activity[field] === "string" && activity[field].trim() === "") {
          delete activity[field];
        }
      }
      this.runRPC(client => client.setActivity(activity));
    }

    clearPresence() {
      if (this.client) this.runRPC(client => client.clearActivity());
    }

    disconnect() {
      const client = this.client;
      this.client = null;
      this.applicationID = "";
      this.ready = false;
      this.error = "";
      if (client) {
        Promise.resolve().then(() => client.destroy()).catch(error => {
          if (!this.client) this.error = String(error.message || error);
        });
      }
    }

    lastError() {
      return this.error;
    }
  }

  Scratch.extensions.register(new RichPresence());
})(Scratch);
