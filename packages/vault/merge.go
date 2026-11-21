package vault

import (
	"slices"
	"strconv"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// mergeInto folds src into dst the way KeePass merges: per entry, the newer modification
// wins and the older version joins its history; deletions win over older edits. It
// reports whether dst changed. Group deletions and moves are not merged.
func mergeInto(dst, src *gokeepasslib.Database) bool {
	changed := false
	deleted := map[gokeepasslib.UUID]time.Time{}
	for _, list := range [][]gokeepasslib.DeletedObjectData{dst.Content.Root.DeletedObjects, src.Content.Root.DeletedObjects} {
		for _, d := range list {
			if d.DeletionTime != nil && d.DeletionTime.Time.After(deleted[d.UUID]) {
				deleted[d.UUID] = d.DeletionTime.Time
			}
		}
	}
	srcDeleted := map[gokeepasslib.UUID]time.Time{}
	for _, d := range src.Content.Root.DeletedObjects {
		if d.DeletionTime != nil {
			srcDeleted[d.UUID] = d.DeletionTime.Time
		}
	}

	mergeGroups(dst, src, &changed)
	have := map[gokeepasslib.UUID]*gokeepasslib.Entry{}
	walkEntries(&dst.Content.Root.Groups[0], func(_ *gokeepasslib.Group, e *gokeepasslib.Entry) { have[e.UUID] = e })
	walkEntries(&src.Content.Root.Groups[0], func(parent *gokeepasslib.Group, e *gokeepasslib.Entry) {
		local, ok := have[e.UUID]
		switch {
		case ok && modified(e).After(modified(local)):
			merged := copyEntry(e, src, dst)
			merged.Histories = mergeHistory(dst, local, &merged)
			if revisionOf(&merged) <= revisionOf(local) {
				setValue(&merged, keyRevision, strconv.FormatInt(revisionOf(local)+1, 10), false)
			}
			*local = merged
			changed = true
		case !ok:
			// An entry edited after another device deleted it comes back, as in KeePass.
			if at, gone := deleted[e.UUID]; gone && !modified(e).After(at) {
				return
			}
			group := findGroup(&dst.Content.Root.Groups[0], parent.UUID)
			if group == nil {
				group = &dst.Content.Root.Groups[0]
			}
			group.Entries = append(group.Entries, copyEntry(e, src, dst))
			changed = true
		}
	})
	removeEntries(&dst.Content.Root.Groups[0], func(e *gokeepasslib.Entry) bool {
		gone, ok := srcDeleted[e.UUID]
		if ok && !modified(e).After(gone) {
			changed = true
			return true
		}
		return false
	})

	union := map[gokeepasslib.UUID]time.Time{}
	for id, at := range deleted {
		union[id] = at
	}
	before := len(dst.Content.Root.DeletedObjects)
	dst.Content.Root.DeletedObjects = dst.Content.Root.DeletedObjects[:0]
	ids := make([]gokeepasslib.UUID, 0, len(union))
	for id := range union {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b gokeepasslib.UUID) int { return union[a].Compare(union[b]) })
	for _, id := range ids {
		at := w.TimeWrapper{Time: union[id]}
		dst.Content.Root.DeletedObjects = append(dst.Content.Root.DeletedObjects, gokeepasslib.DeletedObjectData{UUID: id, DeletionTime: &at})
	}
	if len(dst.Content.Root.DeletedObjects) != before {
		changed = true
	}
	return changed
}

func modified(e *gokeepasslib.Entry) time.Time {
	if e.Times.LastModificationTime == nil {
		return time.Time{}
	}
	return e.Times.LastModificationTime.Time
}

func walkEntries(group *gokeepasslib.Group, visit func(*gokeepasslib.Group, *gokeepasslib.Entry)) {
	for i := range group.Entries {
		visit(group, &group.Entries[i])
	}
	for i := range group.Groups {
		walkEntries(&group.Groups[i], visit)
	}
}

func removeEntries(group *gokeepasslib.Group, remove func(*gokeepasslib.Entry) bool) {
	group.Entries = slices.DeleteFunc(group.Entries, func(e gokeepasslib.Entry) bool { return remove(&e) })
	for i := range group.Groups {
		removeEntries(&group.Groups[i], remove)
	}
}

func findGroup(group *gokeepasslib.Group, id gokeepasslib.UUID) *gokeepasslib.Group {
	if group.UUID == id {
		return group
	}
	for i := range group.Groups {
		if found := findGroup(&group.Groups[i], id); found != nil {
			return found
		}
	}
	return nil
}

// mergeGroups adds groups src has and dst lacks, under the same parent when dst has it.
func mergeGroups(dst, src *gokeepasslib.Database, changed *bool) {
	var walk func(parent *gokeepasslib.Group, group *gokeepasslib.Group)
	walk = func(parent *gokeepasslib.Group, group *gokeepasslib.Group) {
		for i := range group.Groups {
			child := &group.Groups[i]
			if findGroup(&dst.Content.Root.Groups[0], child.UUID) == nil {
				target := findGroup(&dst.Content.Root.Groups[0], group.UUID)
				if target == nil {
					target = &dst.Content.Root.Groups[0]
				}
				empty := *child
				empty.Entries, empty.Groups = nil, nil
				target.Groups = append(target.Groups, empty)
				*changed = true
			}
			walk(group, child)
		}
	}
	walk(nil, &src.Content.Root.Groups[0])
}

// copyEntry deep-copies an entry from src for dst, re-adding its attachments to dst's pool.
func copyEntry(e *gokeepasslib.Entry, src, dst *gokeepasslib.Database) gokeepasslib.Entry {
	c := *e
	c.Values = slices.Clone(e.Values)
	c.CustomData = slices.Clone(e.CustomData)
	c.Times = copyTimes(e.Times)
	c.Binaries = slices.Clone(e.Binaries)
	if src != dst {
		for i, ref := range c.Binaries {
			if b := src.FindBinary(ref.Value.ID); b != nil {
				if content, err := b.GetContentBytes(); err == nil {
					c.Binaries[i].Value.ID = dst.AddBinary(content).ID
				}
			}
		}
	}
	c.Histories = nil
	for _, h := range e.Histories {
		copied := gokeepasslib.History{}
		for i := range h.Entries {
			copied.Entries = append(copied.Entries, copyEntry(&h.Entries[i], src, dst))
		}
		c.Histories = append(c.Histories, copied)
	}
	return c
}

func copyTimes(t gokeepasslib.TimeData) gokeepasslib.TimeData {
	clone := func(tw *w.TimeWrapper) *w.TimeWrapper {
		if tw == nil {
			return nil
		}
		c := *tw
		return &c
	}
	t.CreationTime, t.LastModificationTime = clone(t.CreationTime), clone(t.LastModificationTime)
	t.LastAccessTime, t.ExpiryTime, t.LocationChanged = clone(t.LastAccessTime), clone(t.ExpiryTime), clone(t.LocationChanged)
	return t
}

// mergeHistory keeps every distinct version of both sides, oldest first, within the
// database's history limit. The losing version becomes history.
func mergeHistory(db *gokeepasslib.Database, loser, winner *gokeepasslib.Entry) []gokeepasslib.History {
	versions := []gokeepasslib.Entry{}
	add := func(e gokeepasslib.Entry) {
		e.Histories = nil
		if !slices.ContainsFunc(versions, func(v gokeepasslib.Entry) bool { return modified(&v).Equal(modified(&e)) }) {
			versions = append(versions, e)
		}
	}
	for _, list := range [][]gokeepasslib.History{loser.Histories, winner.Histories} {
		for _, h := range list {
			for _, e := range h.Entries {
				add(e)
			}
		}
	}
	add(copyEntry(loser, db, db))
	slices.SortFunc(versions, func(a, b gokeepasslib.Entry) int { return modified(&a).Compare(modified(&b)) })
	return []gokeepasslib.History{{Entries: trimHistory(db, versions)}}
}

func trimHistory(db *gokeepasslib.Database, versions []gokeepasslib.Entry) []gokeepasslib.Entry {
	limit := int(db.Content.Meta.HistoryMaxItems)
	if limit >= 0 && len(versions) > limit {
		versions = versions[len(versions)-limit:]
	}
	return versions
}
