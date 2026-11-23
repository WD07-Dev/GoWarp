package vm

import (
	"fmt"
	"strings"

	"gowarp/core/extension"
)

// Each service owns its argument validation and optional per-runtime cleanup.
type extensionAPI struct {
	call  func(*extensionContext, string, []any) (any, error)
	close func(*Runtime)
}

var hostAPIs = map[string]extensionAPI{
	"procedures": {
		call: (*extensionContext).proceduresCall,
	},
	"graphics": {
		call: (*extensionContext).graphicsCall,
	},
	"graphics.surface": {
		call: (*extensionContext).penSurfaceCall,
	},
	"graphics.shader": {
		call: (*extensionContext).shaderCall,
	},
	"graphics.text": {
		call: (*extensionContext).textCall,
	},
	"camera": {
		call: (*extensionContext).graphicsCall,
	},
	"canvas": {
		call: (*extensionContext).graphicsCall,
	},
	"stretch": {
		call: (*extensionContext).graphicsCall,
	},
	"audio": {
		call: (*extensionContext).audioCall,
	},
	"options": {
		call: (*extensionContext).optionsCall,
	},
	"runtime": {
		call: (*extensionContext).logicCall,
	},
	"host": {
		call: (*extensionContext).logicCall,
	},
	"input": {
		call: (*extensionContext).logicCall,
	},
	"target": {
		call: (*extensionContext).logicCall,
	},
	"variable": {
		call: (*extensionContext).logicCall,
	},
	"list": {
		call: (*extensionContext).logicCall,
	},
	"events": {
		call: (*extensionContext).logicCall,
	},
	"project": {
		call: (*extensionContext).logicCall,
	},
	"files": {
		call:  (*extensionContext).filesCall,
		close: (*Runtime).closeFiles,
	},
	"assets": {
		call: (*extensionContext).assetsCall,
	},
}

func (r *Runtime) closeHostAPIs() {
	for _, api := range hostAPIs {
		if api.close != nil {
			api.close(r)
		}
	}
}

func (c *extensionContext) Call(operation string, args ...any) (any, error) {
	if len(c.r.variableObservations) != 0 {
		c.r.readObservedVariables()
		defer c.r.writeObservedVariables()
	}
	for name := operation; ; {
		if api, found := hostAPIs[name]; found {
			return api.call(c, operation, args)
		}
		dot := strings.LastIndexByte(name, '.')
		if dot < 0 {
			break
		}
		name = name[:dot]
	}
	return nil, fmt.Errorf("unsupported extension host operation %q", operation)
}

type hostArgs []any

func (args hostArgs) get(index int) any {
	if index < len(args) {
		return args[index]
	}
	return nil
}

func (args hostArgs) string(index int) string {
	return extension.String(args.get(index))
}

func (args hostArgs) number(index int) float64 {
	return extension.Number(args.get(index))
}
