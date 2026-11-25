/*
@gowarp-config {
    require golang.org/x/net v0.0.0-20211118161319-6a13c67c3ce4
}

@gowarp-go {
// Ping Cloud Data by GarboMuffin, original by TheShovel.
// License: MIT AND MPL-2.0
package extension

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
	e "gowarp/core/extension"
)

type pingResult struct {
	done    chan struct{}
	value   bool
	expires time.Time
}

type pingState struct {
	mu                 sync.Mutex
	ctx                context.Context
	cancel             context.CancelFunc
	closed             bool
	pending            map[string]*pingResult
	computed           map[string]*pingResult
	hold, timeout, ttl time.Duration
}

func newPingState() *pingState {
	ctx, cancel := context.WithCancel(context.Background())
	return &pingState{ctx: ctx, cancel: cancel, pending: map[string]*pingResult{}, computed: map[string]*pingResult{}, hold: 2 * time.Second, timeout: 5 * time.Second, ttl: time.Minute}
}

func (s *pingState) close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.computed = map[string]*pingResult{}
	s.mu.Unlock()
}

func (s *pingState) request(server string) *pingResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if result := s.pending[server]; result != nil {
		return result
	}
	if result := s.computed[server]; result != nil && time.Now().Before(result.expires) {
		return result
	}
	u, err := url.Parse(server)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" || u.User != nil || strings.Contains(server, "#") {
		return nil
	}
	// The desktop extension uses the same Origin as the TurboWarp editor.
	config, err := websocket.NewConfig(server, "https://turbowarp.org")
	if err != nil {
		return nil
	}
	result := &pingResult{done: make(chan struct{})}
	s.pending[server] = result
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, s.timeout)
		defer cancel()
		value := pingWebSocket(ctx, config, s.hold)
		s.mu.Lock()
		defer s.mu.Unlock()
		result.value = value && !s.closed
		result.expires = time.Now().Add(s.ttl)
		delete(s.pending, server)
		if !s.closed {
			s.computed[server] = result
		}
		close(result.done)
	}()
	return result
}

func pingWebSocket(ctx context.Context, config *websocket.Config, hold time.Duration) bool {
	u := config.Location
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "wss" {
			port = "443"
		}
	}
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return false
	}
	defer raw.Close()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			raw.Close()
		case <-finished:
		}
	}()
	deadline, _ := ctx.Deadline()
	if err := raw.SetDeadline(deadline); err != nil {
		return false
	}
	var conn net.Conn = raw
	if u.Scheme == "wss" {
		secure := tls.Client(raw, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			return false
		}
		conn = secure
	}
	ws, err := websocket.NewClient(config, conn)
	if err != nil {
		return false
	}
	defer ws.Close()
	stableAt := time.Now().Add(hold)
	if stableAt.Before(deadline) {
		deadline = stableAt
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return false
	}
	// Read and discard data so close/error frames are observed during the hold.
	_, err = io.Copy(io.Discard, ws)
	netErr, timedOut := err.(net.Error)
	return timedOut && netErr.Timeout() && !time.Now().Before(stableAt) && ctx.Err() == nil
}

func New() e.Extension {
	return e.Extension{
		ID:    "clouddataping",
		State: map[string]any{"ping": newPingState()},
		Events: map[string]e.Handler{
			"dispose": func(c *e.Context) (e.Result, error) {
				if state, ok := c.State["ping"].(*pingState); ok {
					state.close()
				}
				return e.Result{}, nil
			},
		},
		Blocks: []e.Block{{
			Opcode: "clouddataping_ping", Kind: "boolean", Text: "is cloud data server [SERVER] up?", Inputs: []string{"SERVER"},
			Handler: func(c *e.Context) (e.Result, error) {
				if c.Local == nil {
					c.Local = map[string]any{}
				}
				result, started := c.Local["ping"].(*pingResult)
				if !started {
					result = c.State["ping"].(*pingState).request(c.String("SERVER"))
					if result == nil {
						return e.Result{Value: false}, nil
					}
					c.Local["ping"] = result
				}
				select {
				case <-result.done:
					return e.Result{Value: result.value}, nil
				default:
					return e.Result{Again: true, YieldTick: true}, nil
				}
			},
		}},
	}
}
}
*/
// Name: Ping Cloud Data
// ID: clouddataping
// Description: Determine whether a cloud variable server is probably up.
// By: GarboMuffin
// Original: TheShovel
// License: MIT AND MPL-2.0

/* generated l10n code */Scratch.translate.setup({"de":{"_Ping Cloud Data":"Ping Clouddaten","_is cloud data server [SERVER] up?":"Ist Cloud-Datenserver [SERVER] verfügbar?"},"fi":{"_Ping Cloud Data":"Pilvidatatarkastus","_is cloud data server [SERVER] up?":"onko pilvidatapalvelin [SERVER] käytössä?"},"it":{"_Ping Cloud Data":"Ping Dati Cloud","_is cloud data server [SERVER] up?":"il server cloud [SERVER] è attivo"},"ja":{"_Ping Cloud Data":"クラウドデータのPing","_is cloud data server [SERVER] up?":"クラウドサーバー[SERVER]が稼働している"},"ko":{"_Ping Cloud Data":"핑 클라우드 데이터","_is cloud data server [SERVER] up?":"클라우드 데이터 서버 [SERVER]이(가) 활성화인가?"},"nb":{"_is cloud data server [SERVER] up?":"er skydata-serveren [SERVER] oppe?"},"nl":{"_Ping Cloud Data":"Cloudservers pingen","_is cloud data server [SERVER] up?":"is cloud-gegevensserver [SERVER] bereikbaar?"},"ru":{"_Ping Cloud Data":"Пинг Облачных Данных","_is cloud data server [SERVER] up?":"сервер облачных данных [SERVER] в сети?"},"zh-cn":{"_Ping Cloud Data":"检测云数据","_is cloud data server [SERVER] up?":"云服务器[SERVER]可以使用？"}});/* end generated l10n code */(function (Scratch) {
  "use strict";

  /**
   * @typedef CacheEntry
   * @property {number} expires
   * @property {boolean} value
   */

  /** @type {Map<string, Promise<CacheEntry>>} */
  const computing = new Map();
  /** @type {Map<string, CacheEntry>} */
  const computed = new Map();

  /**
   * @param {string} uri
   * @returns {Promise<CacheEntry>}
   */
  const pingWebSocket = async (uri) => {
    if (!(await Scratch.canFetch(uri))) {
      return {
        expires: 0,
        value: false,
      };
    }

    /** @type {WebSocket} */
    let ws;
    try {
      // Permission is checked earlier.
      // eslint-disable-next-line extension/check-can-fetch
      ws = new WebSocket(uri);
    } catch (e) {
      return {
        expires: 0,
        value: false,
      };
    }

    let timeoutId;
    const isUp = await new Promise((resolve) => {
      ws.onopen = () => {
        setTimeout(() => {
          resolve(true);
        }, 2000);
      };
      ws.onclose = () => {
        resolve(false);
      };
      ws.onerror = () => {
        resolve(false);
      };
      timeoutId = setTimeout(() => {
        ws.close();
      }, 5000);
    });

    ws.close();
    clearTimeout(timeoutId);

    return {
      expires: Date.now() + 60000,
      value: isUp,
    };
  };

  /**
   * @param {string} uri
   * @returns {boolean|Promise<boolean>}
   */
  const cachedPingWebSocket = (uri) => {
    const computingEntry = computing.get(uri);
    if (computingEntry) {
      return computingEntry.then((entry) => entry.value);
    }

    const computedEntry = computed.get(uri);
    if (computedEntry && Date.now() < computedEntry.expires) {
      return computedEntry.value;
    }

    const promise = pingWebSocket(uri);
    computing.set(uri, promise);
    return promise.then((entry) => {
      computing.delete(uri);
      computed.set(uri, entry);
      return entry.value;
    });
  };

  class PingUtil {
    getInfo() {
      return {
        id: "clouddataping",
        name: Scratch.translate("Ping Cloud Data"),
        blocks: [
          {
            opcode: "ping",
            blockType: Scratch.BlockType.BOOLEAN,
            text: Scratch.translate("is cloud data server [SERVER] up?"),
            arguments: {
              SERVER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "wss://clouddata.turbowarp.org",
              },
            },
          },
        ],
      };
    }

    ping({ SERVER }) {
      return cachedPingWebSocket(SERVER);
    }
  }

  Scratch.extensions.register(new PingUtil());
})(Scratch);
