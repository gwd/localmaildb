package localmaildb

// Things to handle:
// - Only a single email, top-level has [PATCH] (or [PATCH v2])
// - Thread with [0/N] at the top
// - Thread with [1/N] at the top
func TreeFilterAm(tree *MessageTree) []*MessageTree {
	var mt []*MessageTree

	for _, reply := range tree.Replies {
		message := *reply
		message.Replies = nil

		mt = append(mt, &message)
	}

	return mt
}
