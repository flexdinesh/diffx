package fetcher

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	stagingPrefix  = ".init-"
	evictingPrefix = ".evict-"
)

// repositoryLocks serializes work per repository ID; eviction skips held IDs.
type repositoryLocks struct {
	mu   sync.Mutex
	held map[int64]*repositoryLock
}

type repositoryLock struct {
	sync.Mutex
	users int
}

func (locks *repositoryLocks) lock(id int64) func() {
	locks.mu.Lock()
	entry := locks.held[id]
	if entry == nil {
		entry = &repositoryLock{}
		locks.held[id] = entry
	}
	entry.users++
	locks.mu.Unlock()
	entry.Lock()
	return func() { locks.release(id, entry) }
}

func (locks *repositoryLocks) tryLock(id int64) (func(), bool) {
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if locks.held[id] != nil {
		return nil, false
	}
	entry := &repositoryLock{users: 1}
	entry.Lock()
	locks.held[id] = entry
	return func() { locks.release(id, entry) }, true
}

func (locks *repositoryLocks) release(id int64, entry *repositoryLock) {
	entry.Unlock()
	locks.mu.Lock()
	defer locks.mu.Unlock()
	if entry.users--; entry.users == 0 {
		delete(locks.held, id)
	}
}

// prepareMirrors removes staging and eviction leftovers from an interrupted run.
func prepareMirrors(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), stagingPrefix) || strings.HasPrefix(entry.Name(), evictingPrefix) {
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

type mirrorUsage struct {
	id   int64
	size int64
	used time.Time
}

// enforceDiskLimit removes least recently used mirrors until the directory
// fits its limit. Mirrors in use are kept; they are reconsidered after their job.
func (fetcher *fetcher) enforceDiskLimit() {
	fetcher.evicting.Lock()
	defer fetcher.evicting.Unlock()
	root := fetcher.settings.Mirrors
	entries, err := os.ReadDir(root)
	if err != nil {
		log.Printf("diffx-fetcher: read mirrors: %v", err)
		return
	}
	var total int64
	var mirrors []mirrorUsage
	for _, entry := range entries {
		size := directorySize(filepath.Join(root, entry.Name()))
		total += size
		id, err := strconv.ParseInt(entry.Name(), 10, 64)
		if err != nil || !entry.IsDir() {
			continue
		}
		usage := mirrorUsage{id: id, size: size}
		if info, err := os.Stat(filepath.Join(root, entry.Name(), ".git", usedMarker)); err == nil {
			usage.used = info.ModTime()
		}
		mirrors = append(mirrors, usage)
	}
	sort.Slice(mirrors, func(left, right int) bool { return mirrors[left].used.Before(mirrors[right].used) })
	for _, mirror := range mirrors {
		if total <= fetcher.settings.DiskLimit {
			return
		}
		release, ok := fetcher.locks.tryLock(mirror.id)
		if !ok {
			continue
		}
		err := evict(root, mirror.id)
		release()
		if err != nil {
			log.Printf("diffx-fetcher: evict mirror %d: %v", mirror.id, err)
			continue
		}
		total -= mirror.size
	}
	if total > fetcher.settings.DiskLimit {
		log.Printf("diffx-fetcher: mirrors use %d bytes, over the %d byte limit, while in use", total, fetcher.settings.DiskLimit)
	}
}

// evict renames before deleting so a partial removal is never reused.
func evict(root string, id int64) error {
	name := strconv.FormatInt(id, 10)
	target := filepath.Join(root, evictingPrefix+name+"-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := os.Rename(filepath.Join(root, name), target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.RemoveAll(target)
}

// directorySize skips entries that vanish while a concurrent job writes.
func directorySize(path string) int64 {
	var size int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil
		}
		if info, err := entry.Info(); err == nil {
			size += info.Size()
		}
		return nil
	})
	return size
}
