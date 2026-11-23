package extension

import (
	"fmt"
	"strings"
)

/*
Validate constructs each factory once and checks registration metadata against
the project. requiredIDs correspond to the factories in bundle order. It does
not invoke block handlers or lifecycle events and requires no VM or desktop.
*/
func Validate(bundle Bundle, requiredIDs, usedOpcodes []string) error {
	if len(bundle) != len(requiredIDs) {
		return fmt.Errorf("extension factory count does not match required IDs")
	}
	ids := make(map[string]bool, len(bundle))
	blocks := map[string]bool{}
	for i, factory := range bundle {
		if factory == nil {
			return fmt.Errorf("nil extension factory for %q", requiredIDs[i])
		}
		def := factory()
		if def.ID == "" || strings.Contains(def.ID, "_") || IsBuiltin(def.ID) || ids[def.ID] {
			return fmt.Errorf("invalid or duplicate extension ID %q", def.ID)
		}
		if def.ID != requiredIDs[i] {
			return fmt.Errorf("extension %s: New returned ID %q", requiredIDs[i], def.ID)
		}
		ids[def.ID] = true
		for _, block := range def.Blocks {
			if !strings.HasPrefix(block.Opcode, def.ID+"_") {
				return fmt.Errorf("extension %s: invalid opcode %q", def.ID, block.Opcode)
			}
			if blocks[block.Opcode] {
				return fmt.Errorf("duplicate extension opcode %q", block.Opcode)
			}
			switch block.Kind {
			case "command", "reporter", "boolean", "menu", "branch", "terminal":
				if block.Handler == nil {
					return fmt.Errorf("extension %s: missing handler for %s", def.ID, block.Opcode)
				}
			case "hat":
			default:
				return fmt.Errorf("extension %s: invalid block kind %q", def.ID, block.Kind)
			}
			blocks[block.Opcode] = true
		}
	}
	for _, opcode := range usedOpcodes {
		id, _, _ := strings.Cut(opcode, "_")
		if !IsBuiltin(id) && !blocks[opcode] {
			return fmt.Errorf("project opcode %s has no registered Go extension block", opcode)
		}
	}
	return nil
}

// IsBuiltin reports whether an ID belongs to a reserved Scratch block namespace.
func IsBuiltin(id string) bool {
	switch id {
	case "motion", "looks", "sound", "event", "control", "sensing", "operator", "data", "procedures", "argument", "pen":
		return true
	}
	return false
}
