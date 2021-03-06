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

// Package docstore provides a portable implementation of a document store.
// TODO(jba): link to an explanation of document stores (https://en.wikipedia.org/wiki/Document-oriented_database?)
// TODO(jba): expand package doc to batch other Go CDK APIs.
//
//
// Representing Times
//
// Docstore can store and retrieve values of type time.Time, with two caveats. First,
// the timezone may not be preserved. Second, Docstore guarantees only that time.Time
// values are represented to millisecond precision. Many providers will do better,
// but if you need to be sure that times are stored with nanosecond precision,
// convert the time.Time to another type before storing, and re-create when you
// retrieve it. For instance, you could call the time's UnixNano method to get an
// int64, and get the original time back (in the local timezone) with the time.Unix
// function.
//
//
// Revisions
//
// Docstore gives every document a revision when it is created. Docstore uses the
// field name "DocstoreRevision" (stored in the constant docstore.RevisionField) to
// hold the revision. Whenever a document is modified, its revision changes.
// Revisions can be used for optimistic locking: whenever a Put, Replace, Update or
// Delete action is given a document with a revision, then an error for which
// gcerrors.Code returns FailedPrecondition or NotFound is
// returned if the stored document's revision does not match the given document's.
// Thus a Get followed by one of those write actions will fail if the document was
// changed between the Get and the write.
//
// Since different providers use different types for revisions, the type of the
// revision field is unspecified. When defining a struct for storing docstore data,
// define the field to be of type interface{}. For example,
//    type User { Name string; DocstoreRevision interface{} }
// If a struct doesn't have a DocstoreRevision field, then the logic described above
// won't apply to documents read and written with that struct. All writes with the
// struct will succeed even if the document was changed since the last Get.
package docstore // import "gocloud.dev/internal/docstore"
