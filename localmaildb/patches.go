package localmaildb

import "regexp"

// Things to handle:
// - Only a single email, top-level has [PATCH] (or [PATCH v2])
// - Thread with [0/N] at the top
// - Thread with [1/N] at the top
type PatchMailType int

const (
	PatchMailNone      = PatchMailType(0)    // Mail does not seem to be a patch
	PatchMailSingleton = PatchMailType(iota) // Mail seems to be a singleton patch
	PatchMail0N        = PatchMailType(iota) // Mail seems to be a cover letter (0/N)
	PatchMailMN        = PatchMailType(iota) // Mail seems to be a patch in a series (M/N, with 0<M<=N)
)

var rePatchMail = map[PatchMailType]*regexp.Regexp{
	// PatchMailSingleton: regexp.MustCompile(`^\[(RFC )?PATCH( RFC| v[0-9]+)*\]`),
	// PatchMail0N:        regexp.MustCompile(`^\[(RFC )?PATCH( RFC| v[0-9]+)* 0+/[0-9]+\]`),
	// PatchMailMN:        regexp.MustCompile(`^\[(RFC )?PATCH( RFC| v[0-9]+)* 0*[0-9]+0*/[0-9]+\]`),
	PatchMail0N:        regexp.MustCompile(`^\[(RFC )?PATCH( RFC| v[0-9]+)* 0+/[0-9]+\]`),
	PatchMailMN:        regexp.MustCompile(`^\[(RFC )?PATCH( RFC| v[0-9]+)* 0*[0-9]+0*/[0-9]+\]`),
	PatchMailSingleton: regexp.MustCompile(`^\[(RFC )?PATCH(( RFC| v[0-9]+)*| [a-zA-Z-0-9]+)*\]`),
}

func SubjectDetectPatch(s string) PatchMailType {
	for k, re := range rePatchMail {
		if re.MatchString(s) {
			return k
		}
	}
	return PatchMailNone
}

func appendMessage(mtp *[]*MessageTree, msg *MessageTree) {
	message := *msg
	message.Replies = nil

	*mtp = append(*mtp, &message)
}

func TreeFilterAm(tree *MessageTree) []*MessageTree {
	var mt []*MessageTree

	switch SubjectDetectPatch(tree.Envelope.Subject) {
	case PatchMailNone:
		return nil
	case PatchMailSingleton:
		appendMessage(&mt, tree)
	case PatchMailMN:
		// If the root is M/N, add the root and fall through
		appendMessage(&mt, tree)
		fallthrough
	case PatchMail0N:
		// If the root is 0/N, skip the root, but include all replies which are M/N.
		// If the root is M/N, include all replies which are M/N.
		for _, reply := range tree.Replies {
			if SubjectDetectPatch(reply.Envelope.Subject) == PatchMailMN {
				appendMessage(&mt, reply)
			}
		}
	}

	return mt
}
