package localmaildb

import "testing"

func TestSubjectClassifier(t *testing.T) {
	tests := []struct {
		subject string
		want    PatchMailType
	}{
		{"[PATCH] xen/common: Constify the parameter of _spin_is_locked()", PatchMailSingleton},
		{"[PATCH v2] Add more rules to docs/misra/rules.rst", PatchMailSingleton},
		{"[PATCH RFC] Add more rules to docs/misra/rules.rst", PatchMailSingleton},
		{"[RFC PATCH] Add more rules to docs/misra/rules.rst", PatchMailSingleton},
		{"[PATCH RFC v2] Add more rules to docs/misra/rules.rst", PatchMailSingleton},
		{"[PATCH v4 07/11] xen: add cache coloring allocator for domains", PatchMailMN},
		{"[RFC PATCH 01/10] xen: pci: add per-domain pci list lock", PatchMailMN},
		{"[RFC PATCH 10/10] xen: pci: add per-domain pci list lock", PatchMailMN},
		{"[RFC PATCH 0/8] SVE feature for arm guests", PatchMail0N},
		{"[PATCH v2 00/41] xen/arm: Add Armv8-R64 MPU support to Xen - Part#1", PatchMail0N},
		{"[PATCH XSA-438 v4] x86/shadow: defer releasing of PV's top-level shadow reference", PatchMailSingleton},
	}

	for _, test := range tests {
		got := SubjectDetectPatch(test.subject)
		if got != test.want {
			t.Errorf("ERROR: Subject %s: want %v got %v!", test.subject, test.want, got)
		}
	}

}
