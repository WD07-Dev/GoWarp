package vm

import (
	"strings"

	"gowarp/core/project"
)

const (
	nextLinkReady    uint8 = 1
	branchLinkReady  uint8 = 2
	controlPlanReady uint8 = 8
	inputPlanReady   uint8 = 16
	writePlanReady   uint8 = 32
	calculationReady uint8 = 64
)

// Links cache immutable source locations, never a clone's live data or arguments.
type executionCode struct {
	source *project.Target
	blocks map[string]*executionBlock
}

type executionBlock struct {
	control        *numericExpression
	input          *preparedInput
	directDispatch bool
	code           *executionCode
	id             string
	source         *project.Block
	next           *executionBlock
	branches       [2]*executionBlock
	branchErrors   [2]error
	ready          uint8
	calculation    *calculation
	variableWrite  *variableWrite
	listWrite      *listWrite
	move           *coordinateMove
}

func (r *Runtime) executionCode(source *project.Target) *executionCode {
	if code := r.executionCodes[source]; code != nil {
		return code
	}
	if r.executionCodes == nil {
		r.executionCodes = make(map[*project.Target]*executionCode)
	}
	code := &executionCode{source: source, blocks: make(map[string]*executionBlock)}
	r.executionCodes[source] = code
	return code
}

func (c *executionCode) block(id string) *executionBlock {
	if id == "" {
		return nil
	}
	if block := c.blocks[id]; block != nil {
		return block
	}
	block := &executionBlock{code: c, id: id, source: c.source.Blocks[id]}
	if block.source != nil {
		// Built-ins handled by the main switch need no extension/data lookup.
		prefix, _, _ := strings.Cut(block.source.Opcode, "_")
		switch prefix {
		case "control", "procedures", "event", "looks", "sound":
			block.directDispatch = true
		case "motion":
			switch block.source.Opcode {
			case "motion_ifonedgebounce", "motion_pointtowards_xy", "motion_pointtowards_xyfrom":
			default:
				block.directDispatch = true
			}
		}
	}
	c.blocks[id] = block
	return block
}

func (b *executionBlock) nextBlock() *executionBlock {
	if b.ready&nextLinkReady == 0 {
		b.next = b.code.block(b.source.Next)
		b.ready |= nextLinkReady
	}
	return b.next
}

func (b *executionBlock) branch(name string) (string, *executionBlock, error) {
	index := 0
	if name == "SUBSTACK2" {
		index = 1
	}
	bit := branchLinkReady << index
	if b.ready&bit == 0 {
		id, err := substack(b.source, name)
		b.branches[index], b.branchErrors[index] = b.code.block(id), err
		b.ready |= bit
	}
	link := b.branches[index]
	id := ""
	if link != nil {
		id = link.id
	}
	return id, link, b.branchErrors[index]
}

func (r *Runtime) executionBlock(th *thread) *executionBlock {
	if th.code == nil || th.code.source != th.target.source {
		th.code = r.executionCode(th.target.source)
		th.cursor = nil
	}
	if th.cursor == nil || th.cursor.id != th.pc {
		th.cursor = th.code.block(th.pc)
	}
	return th.cursor
}
