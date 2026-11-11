package workflow

// HideRollback menyembunyikan pasangan (eksekusi yang dibatalkan, rollback sukses
// yang membatalkannya). Pasangan ditentukan lewat execution ID, bukan nama.
// Hanya entri yang ada di `items` yang dihitung; entri gagal tidak pernah
// menyembunyikan apa pun karena tidak berada di history sukses.
func HideRollback(items []HistoryEntry) []HistoryEntry {
	hide := map[int64]bool{}
	for _, it := range items {
		if it.Kind == KindRollback && it.Status == StatusSuccess && it.RevertedExecutionID != nil {
			hide[it.ExecutionID] = true
			hide[*it.RevertedExecutionID] = true
		}
	}
	out := make([]HistoryEntry, 0, len(items))
	for _, it := range items {
		if !hide[it.ExecutionID] {
			out = append(out, it)
		}
	}
	return out
}
