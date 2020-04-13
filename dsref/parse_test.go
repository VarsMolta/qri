package dsref

import (
	"testing"
)

func TestParse(t *testing.T) {
	goodCases := []struct {
		description string
		text        string
		expect      Ref
	}{
		{"human friendly", "abc/my_dataset", Ref{Username: "abc", Name: "my_dataset"}},
		{"full reference", "abc/my_dataset@QmFirst/ipfs/QmSecond", Ref{Username: "abc", Name: "my_dataset", ProfileID: "QmFirst", Path: "/ipfs/QmSecond"}},
		{"right hand side", "@QmFirst/ipfs/QmSecond", Ref{ProfileID: "QmFirst", Path: "/ipfs/QmSecond"}},
		{"just path", "@/ipfs/QmSecond", Ref{Path: "/ipfs/QmSecond"}},
		{"long name", "peer/some_name@/map/QmXATayrFgsS3tpCi2ykfpNJ8uiCWT74dttnvJvVo1J7Rn", Ref{Username: "peer", Name: "some_name", Path: "/map/QmXATayrFgsS3tpCi2ykfpNJ8uiCWT74dttnvJvVo1J7Rn"}},
		{"name-has-dash", "abc/my-dataset", Ref{Username: "abc", Name: "my-dataset"}},
		{"dash-in-username", "some-user/my_dataset", Ref{Username: "some-user", Name: "my_dataset"}},
	}
	for i, c := range goodCases {
		ref, err := Parse(c.text)
		if err != nil {
			t.Errorf("case %d %q error: %s", i, c.description, err)
			continue
		}
		if !ref.Equals(c.expect) {
			t.Errorf("case %d %q mismatch: expect %s, got %s", i, c.description, c.expect, ref)
		}
	}

	badCases := []struct {
		description string
		text        string
		expectErr   string
	}{
		{"missing at", "/ipfs/QmThis", "parsing ref, unexpected character at position 0: '/'"},
		{"invalid base58", "@/ipfs/QmOne", "path contains invalid base58 characters"},
		// TODO(dlong): These error messages could be better
		{"no slash", "foo", "parsing ref, unexpected character at position 0: 'f'"},
		{"http url", "https://apple.com", "parsing ref, unexpected character at position 0: 'h'"},
		{"domain name", "apple.com", "parsing ref, unexpected character at position 0: 'a'"},
		{"local filename", "foo.json", "parsing ref, unexpected character at position 0: 'f'"},
		{"absolute filepath", "/usr/local/bin/file.cbor", "parsing ref, unexpected character at position 0: '/'"},
		{"absolute dirname", "/usr/local/bin", "parsing ref, unexpected character at position 0: '/'"},
		{"dot in dataset", "abc/data.set", "parsing ref, unexpected character at position 8: '.'"},
		{"equals in dataset", "abc/my=ds", "parsing ref, unexpected character at position 6: '='"},
	}
	for i, c := range badCases {
		_, err := Parse(c.text)
		if err == nil || err.Error() != c.expectErr {
			t.Errorf("case %d %q expected error: %q, got %q", i, c.description, c.expectErr, err)
			continue
		}
	}
}

func TestParseBadCase(t *testing.T) {
	ref, err := Parse("test_peer/a_New_Dataset")
	if err != ErrBadCaseName {
		t.Errorf("expected to get error %s, but got %s", ErrBadCaseName, err)
	}
	expect := Ref{Username: "test_peer", Name: "a_New_Dataset"}
	if !ref.Equals(expect) {
		t.Errorf("mismatch: expect %s, got %s", expect, ref)
	}
}

func TestParseHumanFriendly(t *testing.T) {
	goodCases := []struct {
		description string
		text        string
		expect      Ref
	}{
		{"human friendly", "abc/my_dataset", Ref{Username: "abc", Name: "my_dataset"}},
	}
	for i, c := range goodCases {
		ref, err := ParseHumanFriendly(c.text)
		if err != nil {
			t.Errorf("case %d %q error: %s", i, c.description, err)
			continue
		}
		if !ref.Equals(c.expect) {
			t.Errorf("case %d %q mismatch: expect %s, got %s", i, c.description, c.expect, ref)
		}
	}

	badCases := []struct {
		description string
		text        string
		expectErr   string
	}{
		{"full reference", "abc/my_dataset@QmFirst/ipfs/QmSecond", ErrNotHumanFriendly.Error()},
		{"right hand side", "@QmFirst/ipfs/QmSecond", ErrNotHumanFriendly.Error()},
		{"just path", "@/ipfs/QmSecond", ErrNotHumanFriendly.Error()},
		{"missing at", "/ipfs/QmThis", "parsing ref, unexpected character at position 0: '/'"},
		{"invalid base58", "@/ipfs/QmOne", ErrNotHumanFriendly.Error()},
	}
	for i, c := range badCases {
		_, err := ParseHumanFriendly(c.text)
		if err == nil || err.Error() != c.expectErr {
			t.Errorf("case %d %q expected error: %q, got %q", i, c.description, c.expectErr, err)
			continue
		}
	}
}

func TestIsValidName(t *testing.T) {
	goodCases := []struct {
		text string
	}{
		{"abc"},
		{"aDataset"},
		{"a1234"},
		{"a_dataset_name"},
		{"DatasetName"},
	}
	for i, c := range goodCases {
		if !IsValidName(c.text) {
			t.Errorf("case %d %q should be valid", i, c.text)
			continue
		}
	}

	badCases := []struct {
		text string
	}{
		{"_bad"},
		{"1dataset"},
		{"dataset!"},
	}
	for i, c := range badCases {
		if IsValidName(c.text) {
			t.Errorf("case %d %q should not be considered valid", i, c.text)
			continue
		}
	}
}
