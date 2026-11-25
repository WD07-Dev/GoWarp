/*
@gowarp-config {
}

@gowarp-go {
package extension

import (
	e "gowarp/core/extension"
	"math"
	"sort"
	"strconv"
	"strings"
)

func New() e.Extension {
	return e.Extension{ID: "lmsTimers", State: map[string]any{}, Blocks: []e.Block{
		{Opcode: "lmsTimers_whenTimerOp", Kind: "hat", Inputs: []string{"TIMER", "NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			t, ok := timers(c)[c.String("TIMER")]
			if !ok {
				return e.Result{Value: false}, nil
			}
			now, err := clock(c)
			v := value(t, now)
			op := c.Field("OP")
			return e.Result{Value: op == ">" && v > c.Number("NUM") || op == "<" && v < c.Number("NUM")}, err
		}},
		{Opcode: "lmsTimers_changeTimer", Kind: "command", Inputs: []string{"TIMER", "NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			ts, name := timers(c), c.String("TIMER")
			t, ok := ts[name]
			if !ok {
				now, err := clock(c)
				if err != nil {
					return e.Result{}, err
				}
				t.Start = now
				remember(c, name)
			}
			t.Value += c.Number("NUM")
			ts[name] = t
			return e.Result{}, nil
		}},
		{Opcode: "lmsTimers_timerExists", Text: "timer [TIMER] exists?", Kind: "boolean", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			_, ok := timers(c)[c.String("TIMER")]
			return e.Result{Value: ok}, nil
		}},
		{Opcode: "lmsTimers_listExistingTimers", Text: "list existing timers", Kind: "reporter", Handler: func(c *e.Context) (e.Result, error) {
			order, _ := c.State["order"].([]string)
			indices := []string{}
			names := []string{}
			for _, name := range order {
				if _, ok := timers(c)[name]; !ok {
					continue
				}
				n, err := strconv.ParseUint(name, 10, 32)
				if err == nil && n < 4294967295 && strconv.FormatUint(n, 10) == name {
					indices = append(indices, name)
				} else {
					names = append(names, name)
				}
			}
			sort.Slice(indices, func(i, j int) bool {
				a, _ := strconv.ParseUint(indices[i], 10, 32)
				b, _ := strconv.ParseUint(indices[j], 10, 32)
				return a < b
			})
			return e.Result{Value: strings.Join(append(indices, names...), ",")}, nil
		}},

		{Opcode: "lmsTimers_resumeTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			ts, name := timers(c), c.String("TIMER")
			t, ok := ts[name]
			if !ok || !t.Paused {
				return e.Result{}, nil
			}
			now, err := clock(c)
			if err != nil {
				return e.Result{}, err
			}
			t.Start, t.Paused = now, false
			ts[name] = t
			return e.Result{}, nil
		}},

		{Opcode: "lmsTimers_valueOfTimer", Text: "value of timer [TIMER]", Kind: "reporter", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			t, ok := timers(c)[c.String("TIMER")]
			if !ok {
				return e.Result{Value: ""}, nil
			}
			now, err := clock(c)
			return e.Result{Value: value(t, now)}, err
		}},
		{Opcode: "lmsTimers_removeTimers", Kind: "command", Inputs: []string{}, Handler: func(c *e.Context) (e.Result, error) {
			c.State["timers"] = map[string]timer{}
			c.State["order"] = []string{}
			return e.Result{}, nil
		}},
		{Opcode: "lmsTimers_removeTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			name := c.String("TIMER")
			delete(timers(c), name)
			order, _ := c.State["order"].([]string)
			for i, n := range order {
				if n == name {
					c.State["order"] = append(order[:i], order[i+1:]...)
					break
				}
			}
			return e.Result{}, nil
		}},
		{Opcode: "lmsTimers_startResetTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			now, err := clock(c)
			if err == nil {
				remember(c, c.String("TIMER"))
				timers(c)[c.String("TIMER")] = timer{Start: now}
			}
			return e.Result{}, err
		}},
		{Opcode: "lmsTimers_setTimer", Kind: "command", Inputs: []string{"TIMER", "NUM"}, Handler: func(c *e.Context) (e.Result, error) {
			now, err := clock(c)
			if err == nil {
				remember(c, c.String("TIMER"))
				timers(c)[c.String("TIMER")] = timer{Start: now, Value: c.Number("NUM")}
			}
			return e.Result{}, err
		}},
		{Opcode: "lmsTimers_pauseTimer", Kind: "command", Inputs: []string{"TIMER"}, Handler: func(c *e.Context) (e.Result, error) {
			now, err := clock(c)
			if err != nil {
				return e.Result{}, err
			}
			ts := timers(c)
			name := c.String("TIMER")
			if t, ok := ts[name]; ok {
				t.Value = value(t, now)
				t.Paused = true
				ts[name] = t
			}
			return e.Result{}, nil
		}},
	}, Events: map[string]e.Handler{"tick": func(c *e.Context) (e.Result, error) {
		_, err := c.Call("events.pollHats", "lmsTimers_whenTimerOp", "edge")
		return e.Result{}, err
	}, "flag": func(c *e.Context) (e.Result, error) {
		c.State["timers"] = map[string]timer{}
		c.State["order"] = []string{}
		return e.Result{}, nil
	}}}
}

type timer struct {
	Start, Value float64
	Paused       bool
}

func clock(c *e.Context) (float64, error) {
	v, err := c.Call("runtime.monotonicTime")
	return math.Floor(e.Number(v) * 1000), err
}
func timers(c *e.Context) map[string]timer {
	v, ok := c.State["timers"].(map[string]timer)
	if !ok {
		v = map[string]timer{}
		c.State["timers"] = v
	}
	return v
}
func value(t timer, now float64) float64 {
	if t.Paused {
		return t.Value
	}
	return t.Value + (now-t.Start)/1000
}

func remember(c *e.Context, name string) {
	if _, ok := timers(c)[name]; !ok {
		order, _ := c.State["order"].([]string)
		c.State["order"] = append(order, name)
	}
}

}
*/
// Name: More Timers
// ID: lmsTimers
// Description: Control several timers at once.
// By: LilyMakesThings <https://scratch.mit.edu/users/LilyMakesThings/>
// License: MIT AND LGPL-3.0

/* generated l10n code */Scratch.translate.setup({"de":{"_More Timers":"Mehr Stoppuhren"},"fi":{"_More Timers":"Enemmän ajastimia","_change timer [TIMER] by [NUM]":"lisää ajastimeen [TIMER] arvo [NUM]","_list existing timers":"olemassa olevat ajastimet","_pause timer [TIMER]":"keskeytä ajastin [TIMER]","_remove all timers":"poista kaikki ajastimet","_remove timer [TIMER]":"poista ajastin [TIMER]","_resume timer [TIMER]":"jatka ajastinta [TIMER]","_set timer [TIMER] to [NUM]":"aseta ajastin [TIMER] arvoon [NUM]","_start/reset timer [TIMER]":"käynnistä/nollaa ajastin [TIMER]","_timer [TIMER]":"ajastin [TIMER]","_timer [TIMER] exists?":"onko ajastin [TIMER] olemassa?","_when timer [TIMER] [OP] [NUM]":"kun ajastin [TIMER] [OP] [NUM]"},"it":{"_More Timers":"Altri Timer","_change timer [TIMER] by [NUM]":"cambia timer [TIMER] di [NUM]","_list existing timers":"lista timer esistenti","_pause timer [TIMER]":"pausa timer [TIMER]","_remove all timers":"elimina tutti i timer","_remove timer [TIMER]":"elimina timer [TIMER]","_set timer [TIMER] to [NUM]":"imposta timer [TIMER] a [NUM]","_start/reset timer [TIMER]":"avvia/cancella timer [TIMER]","_timer [TIMER] exists?":"timer [TIMER] esiste","_when timer [TIMER] [OP] [NUM]":"quando il timer [TIMER] [OP] [NUM]"},"ja":{"_More Timers":"タイマーの追加","_change timer [TIMER] by [NUM]":"タイマー[TIMER]を[NUM]ずつ変える","_list existing timers":"存在するタイマーのリスト","_pause timer [TIMER]":"タイマー[TIMER]を一時停止する","_remove all timers":"タイマーをすべて削除する","_remove timer [TIMER]":"タイマー[TIMER]を削除する","_resume timer [TIMER]":"タイマー[TIMER]を再開する","_set timer [TIMER] to [NUM]":"タイマー[TIMER]を[NUM]秒にする","_start/reset timer [TIMER]":"タイマー[TIMER]を開始/リセット","_timer [TIMER]":"タイマー[TIMER]","_timer [TIMER] exists?":"タイマー[TIMER]が存在する","_when timer [TIMER] [OP] [NUM]":"[TIMER][OP][NUM]のとき"},"ko":{"_More Timers":"추가 타이머","_change timer [TIMER] by [NUM]":"타이머 [TIMER]을(를) [NUM]만큼 바꾸기","_list existing timers":"존재하는 타이머 목록","_pause timer [TIMER]":"타이머 [TIMER] 일시정지하기","_remove all timers":"모든 타이머 제거하기","_remove timer [TIMER]":"타이머 [TIMER]을(를) 제거하기","_resume timer [TIMER]":"타이머 [TIMER] 재시작하기","_set timer [TIMER] to [NUM]":"타이머 [TIMER]을(를) [NUM](으)로 정하기","_start/reset timer [TIMER]":"타이머 [TIMER] 시작·초기화하기","_timer [TIMER]":"타이머 [TIMER] 값","_timer [TIMER] exists?":"타이머 [TIMER]이(가) 존재하는가?","_when timer [TIMER] [OP] [NUM]":"타이머 [TIMER]이(가) [OP][NUM] 일 때"},"nb":{"_More Timers":"Flere tidtakere"},"nl":{"_More Timers":"Meer klokken","_change timer [TIMER] by [NUM]":"verander klok [TIMER] met [NUM]","_list existing timers":"alle huidige klokken","_pause timer [TIMER]":"pauzeer klok [TIMER]","_remove all timers":"verwijder alle klokken","_remove timer [TIMER]":"verwijder klok [TIMER]","_resume timer [TIMER]":"hervat klok [TIMER]","_set timer [TIMER] to [NUM]":"maak klok [TIMER] [NUM]","_start/reset timer [TIMER]":"start/reset klok [TIMER]","_timer [TIMER]":"klok [TIMER]","_timer [TIMER] exists?":"klok [TIMER] bestaat?","_when timer [TIMER] [OP] [NUM]":"wanneer klok [TIMER] [OP] [NUM]"},"ru":{"_More Timers":"Много Таймеров","_change timer [TIMER] by [NUM]":"изменить таймер [TIMER] на [NUM]","_list existing timers":"список существующих таймеров","_pause timer [TIMER]":"приостановить таймер [TIMER]","_remove all timers":"удалить все таймеры","_remove timer [TIMER]":"удалить таймер [TIMER]","_resume timer [TIMER]":"продолжить таймер [TIMER]","_set timer [TIMER] to [NUM]":"задать таймер [TIMER] на [NUM]","_start/reset timer [TIMER]":"запустить/сбросить таймер [TIMER]","_timer [TIMER]":"таймер [TIMER]","_timer [TIMER] exists?":"таймер [TIMER] существует?","_when timer [TIMER] [OP] [NUM]":"когда таймер [TIMER] [OP] [NUM]"},"tr":{"_More Timers":"Daha Fazla Zamanlayıcı"},"uk":{"_More Timers":"Більше Таймерів"},"zh-cn":{"_More Timers":"更多计时器","_change timer [TIMER] by [NUM]":"将计时器[TIMER]增加[NUM]","_list existing timers":"列出存在的计时器","_pause timer [TIMER]":"暂停计时器[TIMER]","_remove all timers":"删除所有计时器","_remove timer [TIMER]":"删除计时器[TIMER]","_resume timer [TIMER]":"继续计时器[TIMER]","_set timer [TIMER] to [NUM]":"将计时器[TIMER]设为[NUM]","_start/reset timer [TIMER]":"开始/重启计时器[TIMER]","_timer [TIMER]":"计时器[TIMER]","_timer [TIMER] exists?":"计时器[TIMER]存在？","_when timer [TIMER] [OP] [NUM]":"当计时器[TIMER][OP][NUM]"}});/* end generated l10n code */(function (Scratch) {
  "use strict";

  const vm = Scratch.vm;

  /**
   * @typedef Timer
   * @property {number} startTime
   * @property {number} pauseTime
   * @property {boolean} paused
   */

  /** @type {Record<string, Timer>} */
  let timers = Object.create(null);

  /**
   * @param {Timer} timer
   * @return {number}
   */
  const timerValue = (timer) => {
    return (
      ((timer.paused ? 0 : Math.floor(performance.now()) - timer.startTime) +
        timer.pauseTime) /
      1000
    );
  };

  class Timers {
    constructor() {
      vm.runtime.on("PROJECT_START", () => {
        timers = Object.create(null);
      });
    }

    getInfo() {
      return {
        id: "lmsTimers",
        name: Scratch.translate("More Timers"),
        color1: "#5cb1d6",
        color2: "#428faf",
        color3: "#3281a3",
        blocks: [
          {
            opcode: "whenTimerOp",
            blockType: Scratch.BlockType.HAT,
            extensions: ["colours_sensing"],
            text: Scratch.translate("when timer [TIMER] [OP] [NUM]"),
            arguments: {
              TIMER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "timer",
              },
              OP: {
                type: Scratch.ArgumentType.STRING,
                menu: "operation",
              },
              NUM: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: "5",
              },
            },
          },

          "---",

          {
            opcode: "startResetTimer",
            blockType: Scratch.BlockType.COMMAND,
            extensions: ["colours_sensing"],
            text: Scratch.translate("start/reset timer [TIMER]"),
            arguments: {
              TIMER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "timer",
              },
            },
          },
          {
            opcode: "valueOfTimer",
            blockType: Scratch.BlockType.REPORTER,
            extensions: ["colours_sensing"],
            text: Scratch.translate("timer [TIMER]"),
            arguments: {
              TIMER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "timer",
              },
            },
          },

          "---",

          {
            opcode: "pauseTimer",
            blockType: Scratch.BlockType.COMMAND,
            extensions: ["colours_sensing"],
            text: Scratch.translate("pause timer [TIMER]"),
            arguments: {
              TIMER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "timer",
              },
            },
          },
          {
            opcode: "resumeTimer",
            blockType: Scratch.BlockType.COMMAND,
            extensions: ["colours_sensing"],
            text: Scratch.translate("resume timer [TIMER]"),
            arguments: {
              TIMER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "timer",
              },
            },
          },

          "---",

          {
            opcode: "setTimer",
            blockType: Scratch.BlockType.COMMAND,
            extensions: ["colours_sensing"],
            text: Scratch.translate("set timer [TIMER] to [NUM]"),
            arguments: {
              TIMER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "timer",
              },
              NUM: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: "10",
              },
            },
          },
          {
            opcode: "changeTimer",
            blockType: Scratch.BlockType.COMMAND,
            extensions: ["colours_sensing"],
            text: Scratch.translate("change timer [TIMER] by [NUM]"),
            arguments: {
              TIMER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "timer",
              },
              NUM: {
                type: Scratch.ArgumentType.NUMBER,
                defaultValue: "10",
              },
            },
          },

          "---",

          {
            opcode: "removeTimer",
            blockType: Scratch.BlockType.COMMAND,
            extensions: ["colours_sensing"],
            text: Scratch.translate("remove timer [TIMER]"),
            arguments: {
              TIMER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "timer",
              },
            },
          },
          {
            opcode: "removeTimers",
            blockType: Scratch.BlockType.COMMAND,
            extensions: ["colours_sensing"],
            text: Scratch.translate("remove all timers"),
          },
          {
            opcode: "timerExists",
            blockType: Scratch.BlockType.BOOLEAN,
            extensions: ["colours_sensing"],
            text: Scratch.translate("timer [TIMER] exists?"),
            arguments: {
              TIMER: {
                type: Scratch.ArgumentType.STRING,
                defaultValue: "timer",
              },
            },
          },
          {
            opcode: "listExistingTimers",
            blockType: Scratch.BlockType.REPORTER,
            extensions: ["colours_sensing"],
            text: Scratch.translate("list existing timers"),
            disableMonitor: false,
          },
        ],
        menus: {
          operation: {
            // false for Scratch parity
            acceptReporters: false,
            items: [">", "<"],
          },
        },
      };
    }

    whenTimerOp(args) {
      if (!timers[args.TIMER]) return false;
      const value = timerValue(timers[args.TIMER]);
      if (args.OP === ">") return value > args.NUM;
      if (args.OP === "<") return value < args.NUM;
      return false;
    }

    startResetTimer(args) {
      timers[args.TIMER] = {
        startTime: Math.floor(performance.now()),
        pauseTime: 0,
        paused: false,
      };
    }

    pauseTimer(args) {
      const timer = timers[args.TIMER];
      if (!timer) return;
      timer.pauseTime = timerValue(timer) * 1000;
      timer.paused = true;
    }

    resumeTimer(args) {
      const timer = timers[args.TIMER];
      if (!timer) return;
      if (timer.paused === false) return;
      timer.paused = false;
      timer.startTime = Math.floor(performance.now());
    }

    valueOfTimer(args) {
      if (!timers[args.TIMER]) return "";
      return timerValue(timers[args.TIMER]);
    }

    setTimer(args) {
      timers[args.TIMER] = {
        paused: false,
        startTime: Math.floor(performance.now()),
        pauseTime: Scratch.Cast.toNumber(args.NUM) * 1000,
      };
    }

    changeTimer(args) {
      if (!timers[args.TIMER]) this.startResetTimer(args);
      timers[args.TIMER].pauseTime += Scratch.Cast.toNumber(args.NUM) * 1000;
    }

    removeTimers(args) {
      timers = Object.create(null);
    }

    removeTimer(args) {
      Reflect.deleteProperty(timers, args.TIMER);
    }

    timerExists(args) {
      return !!timers[args.TIMER];
    }

    listExistingTimers(args) {
      return Object.keys(timers).join(",");
    }
  }

  // "Extension" option reimplementation by Xeltalliv
  // https://github.com/Xeltalliv/extensions/blob/examples/examples/extension-colors.js

  // const cbfsb = Scratch.vm.runtime._convertBlockForScratchBlocks.bind(Scratch.vm.runtime);
  // Scratch.vm.runtime._convertBlockForScratchBlocks = function(blockInfo, categoryInfo) {
  //   const res = cbfsb(blockInfo, categoryInfo);
  //   if (blockInfo.extensions) {
  //     if (!res.json.extensions) res.json.extensions = [];
  //     res.json.extensions.push(...blockInfo.extensions);
  //   }
  //   return res;
  // };

  Scratch.extensions.register(new Timers());
})(Scratch);
