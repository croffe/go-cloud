// Copyright 2019 The Go Cloud Development Kit Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package drivertest provides a conformance test for implementations of
// driver.
package drivertest // import "gocloud.dev/internal/docstore/drivertest"

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"gocloud.dev/gcerrors"
	ds "gocloud.dev/internal/docstore"
	"gocloud.dev/internal/docstore/driver"
)

// Harness descibes the functionality test harnesses must provide to run
// conformance tests.
type Harness interface {
	// MakeCollection makes a driver.Collection for testing.
	MakeCollection(context.Context) (driver.Collection, error)

	// Close closes resources used by the harness.
	Close()
}

// HarnessMaker describes functions that construct a harness for running tests.
// It is called exactly once per test; Harness.Close() will be called when the test is complete.
type HarnessMaker func(ctx context.Context, t *testing.T) (Harness, error)

// Enum of types not supported by native codecs. We chose to describe this negatively
// (types that aren't supported rather than types that are) to make the more
// inclusive cases easier to write. A driver can return nil for
// CodecTester.UnsupportedTypes, then add values from this enum one by one until all
// tests pass.
type UnsupportedType int

const (
	// Native codec doesn't support any unsigned integer type
	Uint UnsupportedType = iota
	// Native codec doesn't support any complex type
	Complex
	// Native codec doesn't support arrays
	Arrays
	// Native codec doesn't support full time precision
	NanosecondTimes
)

// CodecTester describes functions that encode and decode values using both the
// docstore codec for a provider, and that provider's own "native" codec.
type CodecTester interface {
	UnsupportedTypes() []UnsupportedType
	NativeEncode(interface{}) (interface{}, error)
	NativeDecode(value, dest interface{}) error
	DocstoreEncode(interface{}) (interface{}, error)
	DocstoreDecode(value, dest interface{}) error
}

// RunConformanceTests runs conformance tests for provider implementations of docstore.
func RunConformanceTests(t *testing.T, newHarness HarnessMaker, ct CodecTester) {
	t.Run("Create", func(t *testing.T) { withCollection(t, newHarness, testCreate) })
	t.Run("Put", func(t *testing.T) { withCollection(t, newHarness, testPut) })
	t.Run("Replace", func(t *testing.T) { withCollection(t, newHarness, testReplace) })
	t.Run("Get", func(t *testing.T) { withCollection(t, newHarness, testGet) })
	t.Run("Delete", func(t *testing.T) { withCollection(t, newHarness, testDelete) })
	t.Run("Update", func(t *testing.T) { withCollection(t, newHarness, testUpdate) })
	t.Run("Data", func(t *testing.T) { withCollection(t, newHarness, testData) })
	t.Run("Codec", func(t *testing.T) { testCodec(t, ct) })
}

const KeyField = "_id"

func withCollection(t *testing.T, newHarness HarnessMaker, f func(*testing.T, *ds.Collection)) {
	ctx := context.Background()
	h, err := newHarness(ctx, t)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	dc, err := h.MakeCollection(ctx)
	if err != nil {
		t.Fatal(err)
	}
	coll := ds.NewCollection(dc)
	f(t, coll)
}

type docmap = map[string]interface{}

var nonexistentDoc = docmap{KeyField: "doesNotExist"}

func testCreate(t *testing.T, coll *ds.Collection) {
	ctx := context.Background()
	named := docmap{KeyField: "testCreate1", "b": true}
	unnamed := docmap{"b": false}
	// Attempt to clean up
	defer func() {
		_, _ = coll.Actions().Delete(named).Delete(unnamed).Do(ctx)
	}()

	createThenGet := func(doc docmap) {
		t.Helper()
		if err := coll.Create(ctx, doc); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got := docmap{KeyField: doc[KeyField]}
		if err := coll.Get(ctx, got); err != nil {
			t.Fatalf("Get: %v", err)
		}
		// got has a revision field that doc doesn't have.
		doc[ds.RevisionField] = got[ds.RevisionField]
		if diff := cmp.Diff(got, doc); diff != "" {
			t.Fatal(diff)
		}
	}

	createThenGet(named)
	createThenGet(unnamed)

	// Can't create an existing doc.
	if err := coll.Create(ctx, named); err == nil {
		t.Error("got nil, want error")
	}
}

func testPut(t *testing.T, coll *ds.Collection) {
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	named := docmap{KeyField: "testPut1", "b": true}
	// Create a new doc.
	must(coll.Put(ctx, named))
	got := docmap{KeyField: named[KeyField]}
	must(coll.Get(ctx, got))
	named[ds.RevisionField] = got[ds.RevisionField] // copy returned revision field
	if diff := cmp.Diff(got, named); diff != "" {
		t.Fatalf(diff)
	}

	// Replace an existing doc.
	named["b"] = false
	must(coll.Put(ctx, named))
	must(coll.Get(ctx, got))
	named[ds.RevisionField] = got[ds.RevisionField]
	if diff := cmp.Diff(got, named); diff != "" {
		t.Fatalf(diff)
	}

	t.Run("revision", func(t *testing.T) {
		testRevisionField(t, coll, func(dm docmap) error {
			return coll.Put(ctx, dm)
		})
	})
}

func testReplace(t *testing.T, coll *ds.Collection) {
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	doc1 := docmap{KeyField: "testReplace", "s": "a"}
	must(coll.Put(ctx, doc1))
	doc1["s"] = "b"
	must(coll.Replace(ctx, doc1))
	got := docmap{KeyField: doc1[KeyField]}
	must(coll.Get(ctx, got))
	doc1[ds.RevisionField] = got[ds.RevisionField] // copy returned revision field
	if diff := cmp.Diff(got, doc1); diff != "" {
		t.Fatalf(diff)
	}
	// Can't replace a nonexistent doc.
	if err := coll.Replace(ctx, nonexistentDoc); err == nil {
		t.Fatal("got nil, want error")
	}

	t.Run("revision", func(t *testing.T) {
		testRevisionField(t, coll, func(dm docmap) error {
			return coll.Replace(ctx, dm)
		})
	})

}

func testGet(t *testing.T, coll *ds.Collection) {
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	doc := docmap{
		KeyField: "testGet1",
		"s":      "a string",
		"i":      int64(95),
		"f":      32.3,
	}
	must(coll.Put(ctx, doc))
	// If only the key fields are present, the full document is populated.
	got := docmap{KeyField: doc[KeyField]}
	must(coll.Get(ctx, got))
	doc[ds.RevisionField] = got[ds.RevisionField] // copy returned revision field
	if diff := cmp.Diff(got, doc); diff != "" {
		t.Error(diff)
	}
	// TODO(jba): test with field paths
}

func testDelete(t *testing.T, coll *ds.Collection) {
	ctx := context.Background()
	doc := docmap{KeyField: "testDelete"}
	if n, err := coll.Actions().Put(doc).Delete(doc).Do(ctx); err != nil {
		t.Fatalf("after %d successful actions: %v", n, err)
	}
	// The document should no longer exist.
	if err := coll.Get(ctx, doc); err == nil {
		t.Error("want error, got nil")
	}
	// Delete doesn't fail if the doc doesn't exist.
	if err := coll.Delete(ctx, nonexistentDoc); err != nil {
		t.Errorf("delete nonexistent doc: want nil, got %v", err)
	}

	// Delete will fail if the revision field is mismatched.
	got := docmap{KeyField: doc[KeyField]}
	if _, err := coll.Actions().Put(doc).Get(got).Do(ctx); err != nil {
		t.Fatal(err)
	}
	doc["x"] = "y"
	if err := coll.Put(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if err := coll.Delete(ctx, got); gcerrors.Code(err) != gcerrors.FailedPrecondition {
		t.Errorf("got %v, want FailedPrecondition", err)
	}
}

func testUpdate(t *testing.T, coll *ds.Collection) {
	ctx := context.Background()
	doc := docmap{KeyField: "testUpdate", "a": "A", "b": "B"}
	if err := coll.Put(ctx, doc); err != nil {
		t.Fatal(err)
	}

	got := docmap{KeyField: doc[KeyField]}
	_, err := coll.Actions().Update(doc, ds.Mods{
		"a": "X",
		"b": nil,
		"c": "C",
	}).Get(got).Do(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := docmap{
		KeyField:         doc[KeyField],
		ds.RevisionField: got[ds.RevisionField],
		"a":              "X",
		"c":              "C",
	}
	if !cmp.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// TODO(jba): test that empty mods is a no-op.

	// Can't update a nonexistent doc.
	if err := coll.Update(ctx, nonexistentDoc, ds.Mods{"x": "y"}); err == nil {
		t.Error("nonexistent document: got nil, want error")
	}

	t.Run("revision", func(t *testing.T) {
		testRevisionField(t, coll, func(dm docmap) error {
			return coll.Update(ctx, dm, ds.Mods{"s": "c"})
		})
	})
}

func testRevisionField(t *testing.T, coll *ds.Collection, write func(docmap) error) {
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	doc1 := docmap{KeyField: "testRevisionField", "s": "a"}
	must(coll.Put(ctx, doc1))
	got := docmap{KeyField: doc1[KeyField]}
	must(coll.Get(ctx, got))
	rev, ok := got[ds.RevisionField]
	if !ok || rev == nil {
		t.Fatal("missing revision field")
	}
	got["s"] = "b"
	// A write should succeed, because the document hasn't changed since it was gotten.
	if err := write(got); err != nil {
		t.Fatalf("write with revision field got %v, want nil", err)
	}
	// This write should fail: got's revision field hasn't changed, but the stored document has.
	err := write(got)
	if c := gcerrors.Code(err); c != gcerrors.FailedPrecondition && c != gcerrors.NotFound {
		t.Errorf("write with old revision field: got %v, wanted FailedPrecondition or NotFound", err)
	}
}

func testData(t *testing.T, coll *ds.Collection) {
	// All Go integer types are supported, but they all come back as int64.
	ctx := context.Background()
	for _, test := range []struct {
		in, want interface{}
	}{
		{int(-1), int64(-1)},
		{int8(-8), int64(-8)},
		{int16(-16), int64(-16)},
		{int32(-32), int64(-32)},
		{int64(-64), int64(-64)},
		{uint(1), int64(1)},
		{uint8(8), int64(8)},
		{uint16(16), int64(16)},
		{uint32(32), int64(32)},
		{uint64(64), int64(64)},
		{float32(3.5), float64(3.5)},
		{[]byte{0, 1, 2}, []byte{0, 1, 2}},
	} {
		doc := docmap{KeyField: "testData", "val": test.in}
		got := docmap{KeyField: doc[KeyField]}
		if _, err := coll.Actions().Put(doc).Get(got).Do(ctx); err != nil {
			t.Fatal(err)
		}
		want := docmap{
			"val":            test.want,
			KeyField:         doc[KeyField],
			ds.RevisionField: got[ds.RevisionField],
		}
		if len(got) != len(want) {
			t.Errorf("%v: got %v, want %v", test.in, got, want)
		} else if g := got["val"]; !cmp.Equal(g, test.want) {
			t.Errorf("%v: got %v (%T), want %v (%T)", test.in, g, g, test.want, test.want)
		}
	}

	// TODO: strings: valid vs. invalid unicode

}

func testCodec(t *testing.T, ct CodecTester) {
	if ct == nil {
		t.Skip("no CodecTester")
	}
	// A time with non-zero milliseconds, but zero nanoseconds.
	milliTime := time.Date(2019, time.March, 27, 0, 0, 0, 5*1e6, time.UTC)
	// A time with non-zero nanoseconds.
	nanoTime := time.Date(2019, time.March, 27, 0, 0, 0, 5*1e6+7, time.UTC)

	check := func(in, dec interface{}, encode func(interface{}) (interface{}, error), decode func(interface{}, interface{}) error) {
		t.Helper()
		enc, err := encode(in)
		if err != nil {
			t.Fatalf("%+v", err)
		}
		if err := decode(enc, dec); err != nil {
			t.Fatalf("%+v", err)
		}
		if diff := cmp.Diff(in, dec); diff != "" {
			t.Error(diff)
		}
	}

	// A round trip with the docstore codec should work for all docstore-supported types,
	// regardless of native driver support.
	type DocstoreRoundTrip struct {
		N  *int
		I  int
		U  uint
		F  float64
		C  complex128
		St string
		B  bool
		By []byte
		L  []int
		A  [2]int
		M  map[string]bool
		P  *string
		T  time.Time
	}
	// TODO(jba): add more fields: structs; embedding.

	s := "bar"
	dsrt := &DocstoreRoundTrip{
		N:  nil,
		I:  1,
		U:  2,
		F:  2.5,
		C:  complex(3.0, 4.0),
		St: "foo",
		B:  true,
		L:  []int{3, 4, 5},
		A:  [2]int{6, 7},
		M:  map[string]bool{"a": true, "b": false},
		By: []byte{6, 7, 8},
		P:  &s,
		T:  milliTime,
	}

	check(dsrt, &DocstoreRoundTrip{}, ct.DocstoreEncode, ct.DocstoreDecode)

	// Test native-to-docstore and docstore-to-native round trips with a smaller set
	// of types.

	// All native codecs should support these types. If one doesn't, remove it from this
	// struct and make a new single-field struct for it.
	type NativeMinimal struct {
		N  *int
		I  int
		F  float64
		St string
		B  bool
		By []byte
		L  []int
		M  map[string]bool
		P  *string
		T  time.Time
	}
	nm := &NativeMinimal{
		N:  nil,
		I:  1,
		F:  2.5,
		St: "foo",
		B:  true,
		L:  []int{3, 4, 5},
		M:  map[string]bool{"a": true, "b": false},
		By: []byte{6, 7, 8},
		P:  &s,
		T:  milliTime,
	}
	check(nm, &NativeMinimal{}, ct.DocstoreEncode, ct.NativeDecode)
	check(nm, &NativeMinimal{}, ct.NativeEncode, ct.DocstoreDecode)

	// Test various other types, unless they are unsupported.
	unsupported := map[UnsupportedType]bool{}
	for _, u := range ct.UnsupportedTypes() {
		unsupported[u] = true
	}

	// Unsigned integers.
	if !unsupported[Uint] {
		type Uint struct {
			U uint
		}
		u := &Uint{10}
		check(u, &Uint{}, ct.DocstoreEncode, ct.NativeDecode)
		check(u, &Uint{}, ct.NativeEncode, ct.DocstoreDecode)
	}

	// Complex numbers.
	if !unsupported[Complex] {
		type Complex struct {
			C complex128
		}
		c := &Complex{complex(11, 12)}
		check(c, &Complex{}, ct.DocstoreEncode, ct.NativeDecode)
		check(c, &Complex{}, ct.NativeEncode, ct.DocstoreDecode)
	}

	// Arrays.
	if !unsupported[Arrays] {
		type Arrays struct {
			A [2]int
		}
		a := &Arrays{[2]int{13, 14}}
		check(a, &Arrays{}, ct.DocstoreEncode, ct.NativeDecode)
		check(a, &Arrays{}, ct.NativeEncode, ct.DocstoreDecode)
	}
	// Nanosecond-precision time.
	type NT struct {
		T time.Time
	}
	nt := &NT{nanoTime}
	if unsupported[NanosecondTimes] {
		// Expect rounding to the nearest millisecond.
		check := func(encode func(interface{}) (interface{}, error), decode func(interface{}, interface{}) error) {
			enc, err := encode(nt)
			if err != nil {
				t.Fatalf("%+v", err)
			}
			var got NT
			if err := decode(enc, &got); err != nil {
				t.Fatalf("%+v", err)
			}
			want := nt.T.Round(time.Millisecond)
			if !got.T.Equal(want) {
				t.Errorf("got %v, want %v", got.T, want)
			}
		}
		check(ct.DocstoreEncode, ct.NativeDecode)
		check(ct.NativeEncode, ct.DocstoreDecode)
	} else {
		// Expect perfect round-tripping of nanosecond times.
		check(nt, &NT{}, ct.DocstoreEncode, ct.NativeDecode)
		check(nt, &NT{}, ct.NativeEncode, ct.DocstoreDecode)
	}
}

// Call when running tests that will be replayed.
// Each seed value will result in UniqueString producing the same sequence of values.
func MakeUniqueStringDeterministicForTesting(seed int64) {
	r := &randReader{r: rand.New(rand.NewSource(seed))}
	uuid.SetRand(r)
}

type randReader struct {
	mu sync.Mutex
	r  *rand.Rand
}

func (r *randReader) Read(buf []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.r.Read(buf)
}
