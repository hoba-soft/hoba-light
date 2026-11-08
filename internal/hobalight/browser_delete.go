package hobalight

import (
	"fmt"
	"io"
	"strings"
)

func (u *browserUI) requestDelete() {
	id, selected, _, err := u.selectedRestore()
	if err != nil {
		return
	}
	candidates := append([]Reference(nil), u.refs...)
	candidates = append(candidates, u.allRefs...)
	if u.search != nil && len(u.search.matches) > 0 {
		candidates = append(candidates, u.search.matches[u.search.cursor].ref)
	}
	for _, ref := range candidates {
		if ref.Session == id {
			u.pendingDelete = &ref
			u.pendingDeletePath = selected
			u.message = ""
			return
		}
	}
}

func (u *browserUI) confirmDelete() {
	ref := *u.pendingDelete
	u.pendingDelete = nil
	if u.pendingDeletePath != "" {
		var result strings.Builder
		next, err := u.store.deletePath(ref.Session, u.pendingDeletePath, &result)
		u.pendingDeletePath = ""
		if err != nil {
			u.message = "Error: " + err.Error()
			return
		}
		refs, err := u.store.sessionReferences()
		if err != nil {
			u.message = "Error: " + err.Error()
			return
		}
		u.replaceReferences(refs)
		if u.sources || u.source != "" {
			u.selectSource(ref.Directory)
		}
		u.ref, u.detail, u.search, u.tree = nil, nil, nil, nil
		u.merged = nil
		u.history, u.searchHistory, u.searchParent = nil, nil, nil
		u.current, u.cursor, u.parents = "", 0, nil
		u.clearComparison()
		for i, candidate := range u.refs {
			if candidate.Session == next {
				u.cursor = i
				break
			}
		}
		u.enter()
		u.message = strings.TrimSpace(result.String())
		return
	}
	if err := u.store.deleteSession(ref.Session, io.Discard); err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	refs, err := u.store.sessionReferences()
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	cursor := u.cursor
	u.replaceReferences(refs)
	u.cursor = min(cursor, max(0, u.count()-1))
	u.search = nil
	u.history, u.searchHistory, u.searchParent = nil, nil, nil
	u.clearComparison()
	u.message = "Deleted session " + ref.Session[:8]
}

func (u *browserUI) requestFileDelete() {
	var id string
	switch {
	case u.search != nil && len(u.search.matches) > 0:
		id = u.search.matches[u.search.cursor].file.id
	case u.detail != nil:
		id = u.detail.id
	case u.ref != nil && len(u.entries) > 0 && !u.entries[u.cursor].directory:
		id = u.entries[u.cursor].file.id
	default:
		u.message = "Select a file to delete from all sessions"
		return
	}
	plan, err := u.store.planFileDeletion(id)
	if err != nil {
		u.message = "Error: " + err.Error()
		return
	}
	if len(plan.sessions) == 0 {
		u.message = "No sessions reference this file"
		return
	}
	u.pendingFileDelete = &plan
	u.deleteOffset = 0
	u.message = ""
}

func (u *browserUI) confirmFileDelete() {
	plan := *u.pendingFileDelete
	u.pendingFileDelete = nil
	var out strings.Builder
	err := u.store.deleteFileConfirmed(plan, &out)
	refs, readErr := u.store.sessionReferences()
	if readErr == nil {
		u.replaceReferences(refs)
		u.ref, u.detail, u.search, u.tree = nil, nil, nil, nil
		u.merged = nil
		u.history, u.searchHistory, u.searchParent = nil, nil, nil
		u.current, u.cursor, u.parents = "", 0, nil
		u.clearComparison()
	}
	if err != nil {
		u.message = "Error: " + err.Error()
	} else if readErr != nil {
		u.message = "Error: " + readErr.Error()
	} else {
		u.message = fmt.Sprintf("Removed file %s from %d sessions", plan.id[:8], len(plan.sessions))
	}
}
