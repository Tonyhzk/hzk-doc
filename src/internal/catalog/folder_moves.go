package catalog

import (
	"errors"
	"io/fs"
	"path"
)

func (s *Store) deleteFolderMoves(from, to string, retained map[string]bool) ([]move, error) {
	entries, err := fs.ReadDir(s.root.FS(), from)
	if err != nil {
		return nil, err
	}
	var moves []move
	for _, entry := range entries {
		source, target := path.Join(from, entry.Name()), path.Join(to, entry.Name())
		if retained[source] {
			continue
		}
		hasRetained := false
		for name := range retained {
			if within(name, source) {
				hasRetained = true
				break
			}
		}
		if entry.IsDir() && hasRetained {
			nested, err := s.deleteFolderMoves(source, target, retained)
			if err != nil {
				return nil, err
			}
			moves = append(moves, nested...)
		} else {
			moves = append(moves, move{From: source, To: target})
		}
	}
	return moves, nil
}

func (s *Store) restoreFolderMoves(from, to string) ([]move, error) {
	if err := s.safePath(to); err != nil {
		return nil, err
	}
	info, err := s.root.Lstat(to)
	if errors.Is(err, fs.ErrNotExist) {
		return []move{{From: from, To: to}}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrConflict
	}
	entries, err := fs.ReadDir(s.root.FS(), from)
	if err != nil {
		return nil, err
	}
	var moves []move
	for _, entry := range entries {
		source, target := path.Join(from, entry.Name()), path.Join(to, entry.Name())
		if entry.IsDir() {
			nested, err := s.restoreFolderMoves(source, target)
			if err != nil {
				return nil, err
			}
			moves = append(moves, nested...)
		} else {
			moves = append(moves, move{From: source, To: target})
		}
	}
	return moves, nil
}
