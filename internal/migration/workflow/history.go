package workflow

// HideRollback menyembunyikan pasangan (eksekusi yang dibatalkan, rollback sukses
// yang membatalkannya). Pasangan ditentukan lewat execution ID, bukan nama.
// Attempt gagal tidak pernah menyembunyikan atau disembunyikan.
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
		// attempt gagal tetap tampil (v4); hanya baris sukses dari pasangan yang disembunyikan
		if it.Status != StatusSuccess || !hide[it.ExecutionID] {
			out = append(out, it)
		}
	}
	return out
}
