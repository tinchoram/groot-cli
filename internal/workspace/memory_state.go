package workspace

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/tinchoram/groot-cli/internal/catalog"
	"github.com/tinchoram/groot-cli/internal/memory"
	"github.com/tinchoram/groot-cli/internal/tool"
)

// memoryState is the memory view of one open conversation: the store it uses,
// the agent that reads it, and whether that agent sees global memory. Global
// access can change while the conversation is open (grant_global_memory), so
// the memory tools are rebuilt from this state.
type memoryState struct {
	store memory.Store
	agent string
	// writesGlobal lets the agent write global memory. Only groot has it.
	writesGlobal bool
	budget       memory.Budget
	global       atomic.Bool
	// tainted and changed are the conversation hooks of the memory tools; see
	// tool.MemoryAccess.
	mu      sync.Mutex
	tainted func() bool
	changed func()
}

// reader is who reads now: the agent, with global memory when it is enabled.
func (m *memoryState) reader() memory.Reader {
	return memory.Reader{Agent: m.agent, Global: m.global.Load()}
}

// setGlobal enables or disables global memory for later reads and tool calls.
func (m *memoryState) setGlobal(on bool) { m.global.Store(on) }

// access is what the memory tools are built with.
func (m *memoryState) access() tool.MemoryAccess {
	m.mu.Lock()
	defer m.mu.Unlock()
	return tool.MemoryAccess{
		Store:          m.store,
		Reader:         m.reader(),
		CanWriteGlobal: m.writesGlobal,
		Tainted:        m.tainted,
		Changed:        m.changed,
	}
}

// readsGlobal says whether an agent reads the global memory of the root
// database: groot always does, and any other agent when its definition enables it.
func readsGlobal(agent string, def catalog.AgentDef) bool {
	return agent == managedAgent || def.GlobalMemory
}

// memoryStateFor builds the memory view of an agent in a space. The root space
// (or a workspace without a home) uses its own database; another space reads
// global memory from the root database. The budget comes from memory.preamble_*.
func (w *Workspace) memoryStateFor(spacePath string, local memory.Store, def catalog.AgentDef, agent string) (*memoryState, error) {
	isRoot := w.opts.UserHome == "" || filepath.Clean(spacePath) == filepath.Clean(w.opts.UserHome)
	root := local
	if !isRoot {
		rs, err := w.storesFor(w.opts.UserHome)
		if err != nil {
			return nil, fmt.Errorf("open root memory: %w", err)
		}
		root = rs.mem
	}
	m := &memoryState{
		agent:        agent,
		writesGlobal: agent == managedAgent,
		budget: memory.Budget{
			Tokens:       w.opts.Config.Memory.PreambleTokens,
			MaxSummaries: w.opts.Config.Memory.PreambleSummaries,
		},
	}
	m.store = spaceMemoryStore(isRoot, local, root, m.writesGlobal)
	m.setGlobal(readsGlobal(agent, def))
	return m, nil
}

// spaceMemoryStore is the memory store of a space. The root space is the root
// database, so it needs no layering. Any other space keeps its sessions and its
// space and agent memory in its own database and reads and writes global memory
// in the root database.
func spaceMemoryStore(isRoot bool, local, root memory.Store, writesGlobal bool) memory.Store {
	if isRoot {
		return local
	}
	return &memory.Layered{Store: local, Global: root, GlobalWrite: writesGlobal}
}
