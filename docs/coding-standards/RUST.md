<!-- Copyright (c) Microsoft Corporation. Licensed under the MIT license. -->

# Pragmatic Rust Guidelines

This file contains all guidelines concatenated for easy reference.

---


# AI Guidelines



## Design with AI use in Mind (M-DESIGN-FOR-AI) { #M-DESIGN-FOR-AI }

<why>To maximize the utility you get from letting agents work in your code base.</why>
<version>0.1</version>

As a general rule, making APIs easier to use for humans also makes them easier to use by AI.
If you follow the guidelines in this book, you should be in good shape.

Rust's strong type system is a boon for agents, as their lack of genuine understanding can often be
counterbalanced by comprehensive compiler checks, which Rust provides in abundance.

With that said, there are a few guidelines which are particularly important to help make AI coding in Rust more effective:

* **Create Idiomatic Rust API Patterns**. The more your APIs, whether public or internal, look and feel like the majority of
Rust code in the world, the better it is for AI. Follow the [Rust API Guidelines](https://rust-lang.github.io/api-guidelines/checklist.html)
along with the guidelines from [Library / UX](../libs/ux).

* **Provide Thorough Docs**. Agents love good detailed docs. Include docs for all of your modules and public items in your crate.
Assume the reader has a solid, but not expert, level of understanding of Rust, and that the reader understands the standard library.
Follow
[C-CRATE-DOC](https://rust-lang.github.io/api-guidelines/checklist.html#c-crate-doc),
[C-FAILURE](https://rust-lang.github.io/api-guidelines/checklist.html#c-failure),
[C-LINK](https://rust-lang.github.io/api-guidelines/checklist.html#c-link), and
[M-MODULE-DOCS](../docs/#M-MODULE-DOCS)
[M-CANONICAL-DOCS](../docs/#M-CANONICAL-DOCS).

* **Provide Thorough Examples**. Your documentation should have directly usable examples, the repository should include more elaborate ones.
Follow
[C-EXAMPLE](https://rust-lang.github.io/api-guidelines/checklist.html#c-example)
[C-QUESTION-MARK](https://rust-lang.github.io/api-guidelines/checklist.html#c-question-mark).

* **Use Strong Types**. Avoid [primitive obsession](https://refactoring.guru/smells/primitive-obsession) by using strong types with strict well-documented semantics.
Follow
[C-NEWTYPE](https://rust-lang.github.io/api-guidelines/checklist.html#c-newtype).

* **Make Your APIs Testable**. Design APIs which allow your customers to test their use of your API in unit tests. This might involve introducing some mocks, fakes,
or cargo features. AI agents need to be able to iterate quickly to prove that the code they are writing that calls your API is working
correctly.

* **Ensure Test Coverage**. Your own code should have good test coverage over observable behavior.
This enables agents to work in a mostly hands-off mode when refactoring.


---


# Application Guidelines



## Applications may use Anyhow or Derivatives (M-APP-ERROR) { #M-APP-ERROR }

<why>To simplify application-level error handling.</why>
<version>0.1</version>

> Note, this guideline is primarily a relaxation and clarification of [M-ERRORS-CANONICAL-STRUCTS].

Applications, and crates in your own repository exclusively used from your application, may use [anyhow](https://github.com/dtolnay/anyhow),
[eyre](https://github.com/eyre-rs/eyre) or similar application-level error crates instead of implementing their own types.

For example, in your application crates you may just re-export and use eyre's common `Result` type, which should be able to automatically
handle all third party library errors, in particular the ones following
[M-ERRORS-CANONICAL-STRUCTS].

```rust,ignore
use eyre::Result;

fn start_application() -> Result<()> {
    start_server()?;
    Ok(())
}
```

Once you selected your application error crate you should switch all application-level errors to that type, and you should not mix multiple
application-level error types.

Libraries (crates used by more than one crate) should always follow [M-ERRORS-CANONICAL-STRUCTS] instead.

[M-ERRORS-CANONICAL-STRUCTS]: ../libs/ux/#M-ERRORS-CANONICAL-STRUCTS



## Use Mimalloc for Apps (M-MIMALLOC-APPS) { #M-MIMALLOC-APPS }

<why>To get significant performance for free.</why>
<version>0.1</version>

Applications should set [mimalloc](https://crates.io/crates/mimalloc) as their global allocator. This usually results in notable performance
increases along allocating hot paths; we have seen up to 25% benchmark improvements.

Changing the allocator only takes a few lines of code. Add mimalloc to your `Cargo.toml` like so:

```toml
[dependencies]
mimalloc = { version = "0.1" } # Or later version if available
```

Then use it from your `main.rs`:

```rust,ignore
use mimalloc::MiMalloc;

#[global_allocator]
static GLOBAL: MiMalloc = MiMalloc;
```


---


# Documentation



## Documentation Has Canonical Sections (M-CANONICAL-DOCS) { #M-CANONICAL-DOCS }

<why>To follow established and expected Rust best practices.</why>
<version>1.0</version>

Public library items must contain the canonical doc sections. The summary sentence must always be present. Extended documentation and examples
are strongly encouraged. The other sections must be present when applicable.

```rust
/// Summary sentence < 15 words.
///
/// Extended documentation in free form.
///
/// # Examples
/// One or more examples that show API usage like so.
///
/// # Errors
/// If fn returns `Result`, list known error conditions
///
/// # Panics
/// If fn may panic, list when this may happen
///
/// # Safety
/// If fn is `unsafe` or may otherwise cause UB, this section must list
/// all conditions a caller must uphold.
///
/// # Abort
/// If fn may abort the process, list when this may happen.
pub fn foo() {}
```

In contrast to other languages, you should not create a table of parameters. Instead parameter use is explained in plain text. In other words, do not

```rust,ignore
/// Copies a file.
///
/// # Parameters
/// - src: The source.
/// - dst: The destination.
fn copy(src: File, dst: File) {}
```

but instead:

```rust,ignore
/// Copies a file from `src` to `dst`.
fn copy(src: File, dst: File) {}
```

### Related Reading

- Function docs include error, panic, and safety considerations ([C-FAILURE](https://rust-lang.github.io/api-guidelines/documentation.html#c-failure))



## Mark `pub use` Items with `#[doc(inline)]` (M-DOC-INLINE) { #M-DOC-INLINE }

<why>To make re-exported items 'fit in' with their non re-exported siblings.</why>
<version>1.0</version>

When publicly re-exporting crate items via `pub use foo::Foo` or `pub use foo::*`, they show up in an opaque re-export block. In most cases, this is not
helpful to the reader:

![TEXT](M-DOC-INLINE_BAD.png)

Instead, you should annotate them with `#[doc(inline)]` at the `use` site, for them to be inlined organically:

```rust,edition2021,ignore
# pub(crate) mod foo { pub struct Foo; }
#[doc(inline)]
pub use foo::*;

// or

#[doc(inline)]
pub use foo::Foo;
```

![TEXT](M-DOC-INLINE_GOOD.png)

This does not apply to `std` or 3rd party types; these should always be re-exported without inlining to make it clear they are external.

> ### <alert></alert> Still avoid glob exports
>
> The `#[doc(inline)]` trick above does not change [M-NO-GLOB-REEXPORTS]; you generally should not re-export items via wildcards.

[M-NO-GLOB-REEXPORTS]: ../libs/resilience/#M-NO-GLOB-REEXPORTS



## First Sentence is One Line; Approx. 15 Words (M-FIRST-DOC-SENTENCE) { #M-FIRST-DOC-SENTENCE }

<why>To make API docs easily skimmable.</why>
<version>1.0</version>

When you document your item, the first sentence becomes the "summary sentence" that is extracted and shown in the module summary:

```rust
/// This is the summary sentence, shown in the module summary.
///
/// This is other documentation. It is only shown in that item's detail view.
/// Sentences here can be as long as you like and it won't cause any issues.
fn some_item() { }
```

Since Rust API documentation is rendered with a fixed max width, there is a naturally preferred sentence length you should not
exceed to keep things tidy on most screens.

If you keep things in a line, your docs will become easily skimmable. Compare, for example, the standard library:

![TEXT](M-FIRST-DOC-SENTENCE_GOOD.png)

Otherwise, you might end up with _widows_ and a generally unpleasant reading flow:

![TEXT](M-FIRST-DOC-SENTENCE_BAD.png)

As a rule of thumb, the first sentence should not exceed **15 words**.



## Has Comprehensive Module Documentation (M-MODULE-DOCS) { #M-MODULE-DOCS }

<why>To allow for better API docs navigation.</why>
<version>1.1</version>

Any public library module must have `//!` module documentation, and the first sentence must follow [M-DOC-FIRST-SENTENCE].

```rust,edition2021,ignore
pub mod ffi {
    //! Contains FFI abstractions.

    pub struct String {};
}
```

The rest of the module documentation should be comprehensive, i.e., cover the most relevant technical aspects of the contained items, including

- what the module contains
- when it should be used, possibly when not
- examples
- subsystem specifications (e.g., `std::fmt` [also describes its formatting language](https://doc.rust-lang.org/stable/std/fmt/index.html#formatting-parameters))
- observable side effects, including what guarantees are made about these, if any
- relevant implementation details, e.g., the used system APIs

 Great examples include:

- [`std::fmt`](https://doc.rust-lang.org/stable/std/fmt/index.html)
- [`std::pin`](https://doc.rust-lang.org/stable/std/pin/index.html)
- [`std::option`](https://doc.rust-lang.org/stable/std/option/index.html)

This does not mean every module should contain all of these items. But if there is something to say about the interaction of the contained types,
their module documentation is the right place.

[M-DOC-FIRST-SENTENCE]: ./#M-DOC-FIRST-SENTENCE


---


# FFI Guidelines



## Isolate DLL State Between FFI Libraries (M-ISOLATE-DLL-STATE) { #M-ISOLATE-DLL-STATE }

<why>To prevent data corruption and undefined behavior.</why>
<version>0.1</version>

When loading multiple Rust-based dynamic libraries (DLLs) within one application, you may only share 'portable' state between these libraries.
Likewise, when authoring such libraries, you must only accept or provide 'portable' data from foreign DLLs.

Portable here means data that is safe and consistent to process regardless of its origin. By definition, this is a subset of FFI-safe types.
A type is portable if it is `#[repr(C)]` (or similarly well-defined), and _all_ of the following:

- It must not have any interaction with any `static` or thread local.
- It must not have any interaction with any `TypeId`.
- It must not contain any value, pointer or reference to any non-portable data (it is valid to point into portable data within non-portable data, such as
  sharing a reference to an ASCII string held in a `Box`).

_Interaction_ means any computational relationship, and therefore also relates to how the type is used. Sending a `u128` between DLLs is OK, using it to
exchange a transmuted `TypeId` isn't.

The underlying issue stems from the Rust compiler treating each DLL as an entirely new compilation artifact, akin to a standalone application. This means each DLL:

- has its own set of `static` and thread-local variables,
- the type layout of any `#[repr(Rust)]` type (the default) can differ between compilations,
- has its own set of unique type IDs, differing from any other DLL.

Notably, this affects:

- ⚠️ any allocated instance, e.g., `String`, `Vec<u8>`, `Box<Foo>`, ...
- ⚠️ any library relying on other statics, e.g., `tokio`, `log`,
- ⚠️ any struct not `#[repr(C)]`,
- ⚠️ any data structure relying on consistent `TypeId`.

In practice, transferring any of the above between libraries leads to data loss, state corruption, and usually undefined behavior.

Take particular note that this may also apply to types and methods that are invisible at the FFI boundary:

```rust,ignore
/// A method in DLL1 that wants to use a common service from DLL2
#[ffi_function]
fn use_common_service(common: &CommonService) {
    // This has at least two issues:
    // - `CommonService`, or ANY type nested deep within might have
    //   a different type layout in DLL2, leading to immediate
    //   undefined behavior (UB) ⚠️
    // - `do_work()` here looks like it will be invoked in DLL2, but
    //   the code executed will actually come from DLL1. This means that
    //   `do_work()` invoked here will see a data structure coming from
    //   DLL2, but will use statics from DLL1 ⚠️
    common.do_work();
}
```


---


# Library Guidelines


---


# Performance Guidelines



## Identify, Profile, Optimize the Hot Path Early (M-HOTPATH) { #M-HOTPATH }

<why>To end up with high performance code.</why>
<version>0.1</version>

You should, early in the development process, identify if your crate is performance or COGS relevant. If it is:

- identify hot paths and create benchmarks around them,
- regularly run a profiler collecting CPU and allocation insights,
- document or communicate the most performance sensitive areas.

For benchmarks we recommend [criterion](https://crates.io/crates/criterion) or [divan](https://crates.io/crates/divan).
If possible, benchmarks should not only measure elapsed wall time, but also used CPU time over all threads (this unfortunately
requires manual work and is not supported out of the box by the common benchmark utils).

Profiling Rust on Windows works out of the box with [Intel VTune](https://www.intel.com/content/www/us/en/developer/tools/oneapi/vtune-profiler.html)
and [Superluminal](https://superluminal.eu/). However, to gain meaningful CPU insights you should enable debug symbols for benchmarks in your `Cargo.toml`:

```toml
[profile.bench]
debug = 1
```

Documenting the most performance sensitive areas helps other contributors take better decision. This can be as simple as
sharing screenshots of your latest profiling hot spots.

### Further Reading

- [Performance Tips](https://cheats.rs/#performance-tips)

> ### <tip></tip> How much faster?
>
> Some of the most common 'language related' issues we have seen include:
>
> - frequent re-allocations, esp. cloned, growing or `format!` assembled strings,
> - short lived allocations over bump allocations or similar,
> - memory copy overhead that comes from cloning Strings and collections,
> - repeated re-hashing of equal data structures
> - the use of Rust's default hasher where collision resistance wasn't an issue
>
> Anecdotally, we have seen ~15% benchmark gains on hot paths where only some of these `String`  problems were
> addressed, and it appears that up to 50% could be achieved in highly optimized versions.



## Optimize for Throughput, Avoid Empty Cycles (M-THROUGHPUT) { #M-THROUGHPUT }

<why>To ensure COGS savings at scale.</why>
<version>0.1</version>

You should optimize your library for throughput, and one of your key metrics should be _items per CPU cycle_.

This does not mean to neglect latency&mdash;after all you can scale for throughput, but not for latency. However,
in most cases you should not pay for latency with _empty cycles_ that come with single-item processing, contended locks and frequent task switching.

Ideally, you should

- partition reasonable chunks of work ahead of time,
- let individual threads and tasks deal with their slice of work independently,
- sleep or yield when no work is present,
- design your own APIs for batched operations,
- perform work via batched APIs where available,
- yield within long individual items, or between chunks of batches (see [M-YIELD-POINTS]),
- exploit CPU caches, temporal and spatial locality.

You should not:

- hot spin to receive individual items faster,
- perform work on individual items if batching is possible,
- do work stealing or similar to balance individual items.

Shared state should only be used if the cost of sharing is less than the cost of re-computation.

[M-YIELD-POINTS]: ./#M-YIELD-POINTS



## Long-Running Tasks Should Have Yield Points. (M-YIELD-POINTS) { #M-YIELD-POINTS }

<why>To ensure you don't starve other tasks of CPU time.</why>
<version>0.2</version>

If you perform long running computations, they should contain `yield_now().await` points.

Your future might be executed in a runtime that cannot work around blocking or long-running tasks. Even then, such tasks are
considered bad design and cause runtime overhead. If your complex task performs I/O regularly it will simply utilize these await points to preempt itself:

```rust, ignore
async fn process_items(items: &[items]) {
    // Keep processing items, the runtime will preempt you automatically.
    for i in items {
        read_item(i).await;
    }
}
```

If your task performs long-running CPU operations without intermixed I/O, it should instead cooperatively yield at regular intervals, to not starve concurrent operations:

```rust, ignore
async fn process_items(zip_file: File) {
    let items = zip_file.read().async;
    for i in items {
        decompress(i);
        yield_now().await;
    }
}
```

If the number and duration of your individual operations are unpredictable you should use APIs such as `has_budget_remaining()` and
related APIs to query your hosting runtime.

> ### <tip></tip> Yield how often?
>
> In a thread-per-core model the overhead of task switching must be balanced against the systemic effects of starving unrelated tasks.
>
> Under the assumption that runtime task switching takes 100's of ns, in addition to the overhead of lost CPU caches,
> continuous execution in between should be long enough that the switching cost becomes negligible (<1%).
>
> Thus, performing 10 - 100μs of CPU-bound work between yield points would be a good starting point.


---


# Safety Guidelines



## Unsafe Implies Undefined Behavior (M-UNSAFE-IMPLIES-UB) { #M-UNSAFE-IMPLIES-UB }

<why>To ensure semantic consistency and prevent warning fatigue.</why>
<version>1.0</version>

The marker `unsafe` may only be applied to functions and traits if misuse implies the risk of undefined behavior (UB).
It must not be used to mark functions that are dangerous to call for other reasons.

```rust
// Valid use of unsafe
unsafe fn print_string(x: *const String) { }

// Invalid use of unsafe
unsafe fn delete_database() { }
```



## Unsafe Needs Reason, Should be Avoided (M-UNSAFE) { #M-UNSAFE }

<why>To prevent undefined behavior, attack surface, and similar 'happy little accidents'.</why>
<version>0.2</version>

You must have a valid reason to use `unsafe`. The only valid reasons are

1) novel abstractions, e.g., a new smart pointer or allocator,
1) performance, e.g., attempting to call `.get_unchecked()`,
1) FFI and platform calls, e.g., calling into C or the kernel, ...

Unsafe code lowers the guardrails used by the compiler, transferring some of the compiler's responsibilities
to the programmer. Correctness of the resulting code relies primarily on catching all mistakes in code review,
which is error-prone. Mistakes in unsafe code may introduce high-severity security vulnerabilities.

You must not use ad-hoc `unsafe` to

- shorten a performant and safe Rust program, e.g., 'simplify' enum casts via `transmute`,
- bypass `Send` and similar bounds, e.g., by doing `unsafe impl Send ...`,
- bypass lifetime requirements via `transmute` and similar.

Ad-hoc here means `unsafe` embedded in otherwise unrelated code. It is of course permissible to create properly designed, sound abstractions doing these things.

In any case, `unsafe` must follow the guidelines outlined below.

### Novel Abstractions

- [ ] Verify there is no established alternative. If there is, prefer that.
- [ ] Your abstraction must be minimal and testable.
- [ ] It must be hardened and tested against ["adversarial code"](https://cheats.rs/#adversarial-code), esp.
  - If they accept closures they must become invalid (e.g., poisoned) if the closure panics
  - They must assume any safe trait is misbehaving, esp. `Deref`, `Clone` and `Drop`.
- [ ] Any use of `unsafe` must be accompanied by plain-text reasoning outlining its safety
- [ ] It must pass [Miri](https://github.com/rust-lang/miri), including adversarial test cases
- [ ] It must follow all other [unsafe code guidelines](https://rust-lang.github.io/unsafe-code-guidelines/)

### Performance

- [ ] Using `unsafe` for performance reasons should only be done after benchmarking
- [ ] Any use of `unsafe` must be accompanied by plain-text reasoning outlining its safety. This applies to both
  calling `unsafe` methods, as well as providing `_unchecked` ones.
- [ ] The code in question must pass [Miri](https://github.com/rust-lang/miri)
- [ ] You must follow the [unsafe code guidelines](https://rust-lang.github.io/unsafe-code-guidelines/)

### FFI

- [ ] We recommend you use an established interop library to avoid `unsafe` constructs
- [ ] You must follow the [unsafe code guidelines](https://rust-lang.github.io/unsafe-code-guidelines/)
- [ ] You must document your generated bindings to make it clear which call patterns are permissible

### Further Reading

- [Nomicon](https://doc.rust-lang.org/nightly/nomicon/)
- [Unsafe Code Guidelines](https://rust-lang.github.io/unsafe-code-guidelines/)
- [Miri](https://github.com/rust-lang/miri)
- ["Adversarial code"](https://cheats.rs/#adversarial-code)



## All Code Must be Sound (M-UNSOUND) { #M-UNSOUND }

<why>To prevent unexpected runtime behavior, leading to potential bugs and incompatibilities.</why>
<version>1.0</version>

Unsound code is seemingly _safe_ code that may produce undefined behavior when called from other safe code, or on its own accord.

> ### <tip></tip> Meaning of 'Safe'
>
> The terms _safe_ and `unsafe` are technical terms in Rust.
>
> A function is _safe_, if its signature does not mark it `unsafe`. That said, _safe_ functions can still be dangerous
> (e.g., `delete_database()`), and `unsafe` ones are, when properly used, usually quite benign (e.g.,`vec.get_unchecked()`).
>
> A function is therefore _unsound_ if it appears _safe_ (i.e., it is not marked `unsafe`), but if _any_ of its calling
> modes would cause undefined behavior. This is to be interpreted in the strictest sense. Even if causing undefined
> behavior is only a 'remote, theoretical possibility' requiring 'weird code', the function is unsound.
>
> Also see [Unsafe, Unsound, Undefined](https://cheats.rs/#unsafe-unsound-undefined).

```rust
// "Safely" converts types
fn unsound_ref<T>(x: &T) -> &u128 {
    unsafe { std::mem::transmute(x) }
}

// "Clever trick" to work around missing `Send` bounds.
struct AlwaysSend<T>(T);
unsafe impl<T> Send for AlwaysSend<T> {}
unsafe impl<T> Sync for AlwaysSend<T> {}
```

Unsound abstractions are never permissible. If you cannot safely encapsulate something, you must expose `unsafe` functions instead, and document proper behavior.

<div class="warning">

No Exceptions

While you may break most guidelines if you have a good enough reason, there are no exceptions in this case: unsound code is never acceptable.

</div>

> ### <tip></tip> It's the Module Boundaries
>
> Note that soundness boundaries equal module boundaries! It is perfectly fine, in an otherwise safe abstraction,
> to have safe functions that rely on behavior guaranteed elsewhere **in the same module**.
>
> ```rust
> struct MyDevice(*const u8);
>
> impl MyDevice {
>     fn new() -> Self {
>        // Properly initializes instance ...
>        # todo!()
>     }
>
>     fn get(&self) -> u8 {
>         // It is perfectly fine to rely on `self.0` being valid, despite this
>         // function in-and-by itself being unable to validate that.
>         unsafe { *self.0 }
>     }
> }
>
> ```


---


# Universal Guidelines



## Names are Free of Weasel Words (M-CONCISE-NAMES) { #M-CONCISE-NAMES }

<why>To improve readability.</why>
<version>1.0</version>

Symbol names, especially types and traits names, should be free of weasel words that do not meaningfully
add information. Common offenders include `Service`, `Manager`, and `Factory`. For example:

While your library may very well contain or communicate with a booking service&mdash;or even hold an `HttpClient`
instance named `booking_service`&mdash;one should rarely encounter a `BookingService` _type_ in code.

An item handling many bookings can just be called `Bookings`. If it does anything more specific, then that quality
should be appended instead. It submits these items elsewhere? Calling it `BookingDispatcher` would be more helpful.

The same is true for `Manager`s. Every code manages _something_, so that moniker is rarely useful. With rare
exceptions, life cycle issues should likewise not be made the subject of some manager. Items are created in whatever
way they are needed, their disposal is governed by `Drop`, and only `Drop`.

Regarding factories, at least the term should be avoided. While the concept `FooFactory` has its use, its canonical
Rust name is `Builder` (compare [M-INIT-BUILDER](../libs/ux/#M-INIT-BUILDER)). A builder that can produce items repeatedly is still a builder.

In addition, accepting factories (builders) as parameters is an unidiomatic import of OO concepts into Rust. If
repeatable instantiation is required, functions should ask for an `impl Fn() -> Foo` over a `FooBuilder` or
similar. In contrast, standalone builders have their use, but primarily to reduce parametric permutation complexity
around optional values (again, [M-INIT-BUILDER](../libs/ux/#M-INIT-BUILDER)).



## Magic Values are Documented (M-DOCUMENTED-MAGIC) { #M-DOCUMENTED-MAGIC }

<why>To ensure maintainability and prevent misunderstandings when refactoring.</why>
<version>1.0</version>

Hardcoded _magic_ values in production code must be accompanied by a comment. The comment should outline:

- why this value was chosen,
- non-obvious side effects if that value is changed,
- external systems that interact with this constant.

You should prefer named constants over inline values.

```rust, ignore
// Bad: it's relatively obvious that this waits for a day, but not why
wait_timeout(60 * 60 * 24).await // Wait at most a day

// Better
wait_timeout(60 * 60 * 24).await // Large enough value to ensure the server
                                 // can finish. Setting this too low might
                                 // make us abort a valid request. Based on
                                 // `api.foo.com` timeout policies.

// Best

/// How long we wait for the server.
///
/// Large enough value to ensure the server
/// can finish. Setting this too low might
/// make us abort a valid request. Based on
/// `api.foo.com` timeout policies.
const UPSTREAM_SERVER_TIMEOUT: Duration = Duration::from_secs(60 * 60 * 24);
```



## Lint Overrides Should Use `#[expect]` (M-LINT-OVERRIDE-EXPECT) { #M-LINT-OVERRIDE-EXPECT }

<why>To prevent the accumulation of outdated lints.</why>
<version>1.0</version>

When overriding project-global lints inside a submodule or item, you should do so via `#[expect]`, not `#[allow]`.

Expected lints emit a warning if the marked warning was not encountered, thus preventing the accumulation of stale lints.
That said, `#[allow]` lints are still useful when applied to generated code, and can appear in macros.

Overrides should be accompanied by a `reason`:

```rust,edition2021
#[expect(clippy::unused_async, reason = "API fixed, will use I/O later")]
pub async fn ping_server() {
  // Stubbed out for now
}
```



## Use Structured Logging with Message Templates (M-LOG-STRUCTURED) { #M-LOG-STRUCTURED }

<why>To minimize the cost of logging and to improve filtering capabilities.</why>
<version>0.1</version>

Logging should use structured events with named properties and message templates following
the [message templates](https://messagetemplates.org/) specification.

> **Note:** Examples use the [`tracing`](https://docs.rs/tracing/) crate's `event!` macro,
but these principles apply to any logging API that supports structured logging (e.g., `log`,
`slog`, custom telemetry systems).

### Avoid String Formatting

String formatting allocates memory at runtime. Message templates defer formatting until viewing time.
We recommend that message template includes all named properties for easier inspection at viewing time.

```rust,ignore
// Bad: String formatting causes allocations
tracing::info!("file opened: {}", path);
tracing::info!(format!("file opened: {}", path));

// Good: Message templates with named properties
event!(
    name: "file.open.success",
    Level::INFO,
    file.path = path.display(),
    "file opened: {{file.path}}",
);
```

> **Note**: Use the `{{property}}` syntax in message templates which preserves the literal text
> while escaping Rust's format syntax. String formatting is deferred until logs are viewed.

### Name Your Events

Use hierarchical dot-notation: `<component>.<operation>.<state>`

```rust,ignore
// Bad: Unnamed events
event!(
    Level::INFO,
    file.path = file_path,
    "file {{file.path}} processed succesfully",
);

// Good: Named events
event!(
    name: "file.processing.success", // event identifier
    Level::INFO,
    file.path = file_path,
    "file {{file.path}} processed succesfully",
);
```

Named events enable grouping and filtering across log entries.

### Follow OpenTelemetry Semantic Conventions

Use [OTel semantic conventions](https://opentelemetry.io/docs/specs/semconv/) for common attributes if needed.
This enables standardization and interoperability.

```rust,ignore
event!(
    name: "file.write.success",
    Level::INFO,
    file.path = path.display(),         // Standard OTel name
    file.size = bytes_written,          // Standard OTel name
    file.directory = dir_path,          // Standard OTel name
    file.extension = extension,         // Standard OTel name
    file.operation = "write",           // Custom name
    "{{file.operation}} {{file.size}} bytes to {{file.path}} in {{file.directory}} extension={{file.extension}}",
);
```

Common conventions:

- HTTP: `http.request.method`, `http.response.status_code`, `url.scheme`, `url.path`, `server.address`
- File: `file.path`, `file.directory`, `file.name`, `file.extension`, `file.size`
- Database: `db.system.name`, `db.namespace`, `db.operation.name`, `db.query.text`
- Errors: `error.type`, `error.message`, `exception.type`, `exception.stacktrace`

### Redact Sensitive Data

Do not log plain sensitive data as this might lead to privacy and security incidents.

```rust,ignore
// Bad: Logs potentially sensitive data
event!(
    name: "file.operation.started",
    Level::INFO,
    user.email = user.email,  // Sensitive data
    file.name = "license.txt",
    "reading file {{file.name}} for user {{user.email}}",
);

// Good: Redact sensitive parts
event!(
    name: "file.operation.started",
    Level::INFO,
    user.email.redacted = redact_email(user.email),
    file.name = "license.txt",
    "reading file {{file.name}} for user {{user.email.redacted}}",
);
```

Sensitive data includes email addresses, file paths revealing user identity, filenames containing secrets or tokens,
file contents with PII, temporary file paths with session IDs and more. Consider using the [`data_privacy`](https://crates.io/crates/data_privacy) crate for consistent redaction.

### Further Reading

- [Message Templates Specification](https://messagetemplates.org/)
- [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/)
- [OWASP Logging Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html)



## Panic Means 'Stop the Program' (M-PANIC-IS-STOP) { #M-PANIC-IS-STOP }

<why>To ensure soundness and predictability.</why>
<version>1.0</version>

Panics are not exceptions. Instead, they suggest immediate program termination.

Although your code must be [panic-safe](https://doc.rust-lang.org/nomicon/exception-safety.html) (i.e., a survived panic may not lead to
inconsistent state), invoking a panic means _this program should stop now_. It is not valid to:

- use panics to communicate (errors) upstream,
- use panics to handle self-inflicted error conditions,
- assume panics will be caught, even by your own code.

For example, if the application calling you is compiled with a `Cargo.toml` containing

```toml
[profile.release]
panic = "abort"
```

then any invocation of panic will cause an otherwise functioning program to needlessly abort. Valid reasons to panic are:

- when encountering a programming error, e.g., `x.expect("must never happen")`,
- anything invoked from const contexts, e.g., `const { foo.unwrap() }`,
- when user requested, e.g., providing an `unwrap()` method yourself,
- when encountering a poison, e.g., by calling `unwrap()` on a lock result (a poisoned lock signals another thread has panicked already).

Any of those are directly or indirectly linked to programming errors.



## Detected Programming Bugs are Panics, Not Errors (M-PANIC-ON-BUG) { #M-PANIC-ON-BUG }

<why>To avoid impossible error handling code and ensure runtime consistency.</why>
<version>1.0</version>

As an extension of [M-PANIC-IS-STOP] above, when an unrecoverable programming error has been
detected, libraries and applications must panic, i.e., request program termination.

In these cases, no `Error` type should be introduced or returned, as any such error could not be acted upon at runtime.

Contract violations, i.e., the breaking of invariants either within a library or by a caller, are programming errors and must therefore panic.

However, what constitutes a violation is situational. APIs are not expected to go out of their way to detect them, as such
checks can be impossible or expensive. Encountering `must_be_even == 3` during an already existing check clearly warrants
a panic, while a function `parse(&str)` clearly must return a `Result`. If in doubt, we recommend you take inspiration from the standard library.

```rust, ignore
// Generally, a function with bad parameters must either
// - Ignore a parameter and/or return the wrong result
// - Signal an issue via Result or similar
// - Panic
// If in this `divide_by` we see that y == 0, panicking is
// the correct approach.
fn divide_by(x: u32, y: u32) -> u32 { ... }

// However, it can also be permissible to omit such checks
// and return an unspecified (but not an undefined) result.
fn divide_by_fast(x: u32, y: u32) -> u32 { ... }

// Here, passing an invalid URI is not a contract violation.
// Since parsing is inherently fallible, a Result must be returned.
fn parse_uri(s: &str) -> Result<Uri, ParseError> { };

```

> ### <tip></tip> Make it 'Correct by Construction'
>
> While panicking on a detected programming error is the 'least bad option', your panic might still ruin someone's day.
> For any user input or calling sequence that would otherwise panic, you should also explore if you can use the type
> system to avoid panicking code paths altogether.

[M-PANIC-IS-STOP]: ../universal/#M-PANIC-IS-STOP



## Public Types are Debug (M-PUBLIC-DEBUG) { #M-PUBLIC-DEBUG }

<why>To simplify debugging and prevent leaking sensitive data.</why>
<version>1.0</version>

All public types exposed by a crate should implement `Debug`. Most types can do so via `#[derive(Debug)]`:

```rust
#[derive(Debug)]
struct Endpoint(String);
```

Types designed to hold sensitive data should also implement `Debug`, but do so via a custom implementation.
This implementation must employ unit tests to ensure sensitive data isn't actually leaked, and will not be in the future.

```rust
use std::fmt::{Debug, Formatter};

struct UserSecret(String);

impl Debug for UserSecret {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(f, "UserSecret(...)")
    }
}

#[test]
fn test() {
    let key = "552d3454-d0d5-445d-ab9f-ef2ae3a8896a";
    let secret = UserSecret(key.to_string());
    let rendered = format!("{:?}", secret);

    assert!(rendered.contains("UserSecret"));
    assert!(!rendered.contains(key));
}
```



## Public Types Meant to be Read are Display (M-PUBLIC-DISPLAY) { #M-PUBLIC-DISPLAY }

<why>To improve usability.</why>
<version>1.0</version>

If your type is expected to be read by upstream consumers, be it developers or end users, it should implement `Display`. This in particular includes:

- Error types, which are mandated by `std::error::Error` to implement `Display`
- Wrappers around string-like data

Implementations of `Display` should follow Rust customs; this includes rendering newlines and escape sequences.
The handling of sensitive data outlined in [M-PUBLIC-DEBUG] applies analogously.

[M-PUBLIC-DEBUG]: ./#M-PUBLIC-DEBUG



## Prefer Regular over Associated Functions (M-REGULAR-FN) { #M-REGULAR-FN }

<why>To improve readability.</why>
<version>1.0</version>

Associated functions should primarily be used for instance creation, not general purpose computation.

In contrast to some OO languages, regular functions are first-class citizens in Rust and need no module or _class_ to host them. Functionality that
does not clearly belong to a receiver should therefore not reside in a type's `impl` block:

```rust, ignore
struct Database {}

impl Database {
    // Ok, associated function creates an instance
    fn new() -> Self {}

    // Ok, regular method with `&self` as receiver
    fn query(&self) {}

    // Not ok, this function is not directly related to `Database`,
    // it should therefore not live under `Database` as an associated
    // function.
    fn check_parameters(p: &str) {}
}

// As a regular function this is fine
fn check_parameters(p: &str) {}
```

Regular functions are more idiomatic, and reduce unnecessary noise on the caller side. Associated trait functions are perfectly idiomatic though:

```rust
pub trait Default {
    fn default() -> Self;
}

struct Foo;

impl Default for Foo {
    fn default() -> Self { Self }
}
```



## If in Doubt, Split the Crate (M-SMALLER-CRATES) { #M-SMALLER-CRATES }

<why>To improve compile times and modularity.</why>
<version>1.0</version>

You should err on the side of having too many crates rather than too few, as this leads to dramatic compile time improvements—especially
during the development of these crates—and prevents cyclic component dependencies.

Essentially, if a submodule can be used independently, its contents should be moved into a separate crate.

Performing this crate split may cause you to lose access to some `pub(crate)` fields or methods. In many situations, this is a desirable
side-effect and should prompt you to design more flexible abstractions that would give your users similar affordances.

In some cases, it is desirable to re-join individual crates back into a single _umbrella crate_, such as when dealing with proc macros, or runtimes.
Functionality split for technical reasons (e.g., a `foo_proc` proc macro crate) should always be re-exported. Otherwise, re-exports should be used sparingly.

> ### <tip></tip> Features vs. Crates
>
> As a rule of thumb, crates are for items that can reasonably be used on their own. Features should unlock extra functionality that
> can't live on its own. In the case of umbrella crates, see below, features may also be used to enable constituents (but then that functionality
> was extracted into crates already).
>
> For example, if you defined a `web` crate with the following modules, users only needing client calls would also have to pay for the compilation of server code:
>
> ```text
> web::server
> web::client
> web::protocols
> ```
>
> Instead, you should introduce individual crates that give users the ability to pick and choose:
>
> ```text
> web_server
> web_client
> web_protocols
> ```



## Use Static Verification (M-STATIC-VERIFICATION) { #M-STATIC-VERIFICATION }

<why>To ensure consistency and avoid common issues.</why>
<version>1.0</version>

Projects should use the following static verification tools to help maintain the quality of the code. These tools can be
configured to run on a developer's machine during normal work, and should be used as part of check-in gates.

* [compiler lints](https://doc.rust-lang.org/rustc/lints/index.html) offer many lints to avoid bugs and improve code quality.
* [clippy lints](https://doc.rust-lang.org/clippy/) contain hundreds of lints to avoid bugs and improve code quality.
* [rustfmt](https://github.com/rust-lang/rustfmt) ensures consistent source formatting.
* [cargo-audit](https://crates.io/crates/cargo-audit) verifies crate dependencies for security vulnerabilities.
* [cargo-hack](https://crates.io/crates/cargo-hack) validates that all combinations of crate features work correctly.
* [cargo-udeps](https://crates.io/crates/cargo-udeps) detects unused dependencies in Cargo.toml files.
* [miri](https://github.com/rust-lang/miri) validates the correctness of unsafe code.

### Compiler Lints

The Rust compiler generally produces exceptionally good diagnostics. In addition to the default set of diagnostics, projects
should explicitly enable the following set of compiler lints:

```toml
[lints.rust]
ambiguous_negative_literals = "warn"
missing_debug_implementations = "warn"
redundant_imports = "warn"
redundant_lifetimes = "warn"
trivial_numeric_casts = "warn"
unsafe_op_in_unsafe_fn = "warn"
unused_lifetimes = "warn"
```

### Clippy Lints

For clippy, projects should enable all major lint categories, and additionally enable some lints from the `restriction` lint group.
Undesired lints (e.g., numeric casts) can be opted back out of on a case-by-case basis:

```toml
[lints.clippy]
cargo = { level = "warn", priority = -1 }
complexity = { level = "warn", priority = -1 }
correctness = { level = "warn", priority = -1 }
pedantic = { level = "warn", priority = -1 }
perf = { level = "warn", priority = -1 }
style = { level = "warn", priority = -1 }
suspicious = { level = "warn", priority = -1 }
# nursery = { level = "warn", priority = -1 }  # optional, might cause more false positives

# These lints are from the `restriction` lint group and prevent specific
# constructs being used in source code in order to drive up consistency,
# quality, and brevity
allow_attributes_without_reason = "warn"
as_pointer_underscore = "warn"
assertions_on_result_states = "warn"
clone_on_ref_ptr = "warn"
deref_by_slicing = "warn"
disallowed_script_idents = "warn"
empty_drop = "warn"
empty_enum_variants_with_brackets = "warn"
empty_structs_with_brackets = "warn"
fn_to_numeric_cast_any = "warn"
if_then_some_else_none = "warn"
map_err_ignore = "warn"
redundant_type_annotations = "warn"
renamed_function_params = "warn"
semicolon_outside_block = "warn"
string_to_string = "warn"
undocumented_unsafe_blocks = "warn"
unnecessary_safety_comment = "warn"
unnecessary_safety_doc = "warn"
unneeded_field_pattern = "warn"
unused_result_ok = "warn"

# May cause issues with structured logging otherwise.
literal_string_with_formatting_args = "allow"

# Define custom opt outs here
# ...
```



## Follow the Upstream Guidelines (M-UPSTREAM-GUIDELINES) { #M-UPSTREAM-GUIDELINES }

<why>To avoid repeating mistakes the community has already learned from, and to have a codebase that does not surprise users and contributors.</why>
<version>1.0</version>

The guidelines in this book complement existing Rust guidelines, in particular:

- [Rust API Guidelines](https://rust-lang.github.io/api-guidelines/checklist.html)
- [Rust Style Guide](https://doc.rust-lang.org/nightly/style-guide/)
- [Rust Design Patterns](https://rust-unofficial.github.io/patterns//intro.html)
- [Rust Reference - Undefined Behavior](https://doc.rust-lang.org/reference/behavior-considered-undefined.html)

We recommend you read through these as well, and apply them in addition to this book's items. Pay special attention to the ones below, as they are frequently forgotten:

- [ ] [C-CONV](https://rust-lang.github.io/api-guidelines/naming.html#ad-hoc-conversions-follow-as_-to_-into_-conventions-c-conv) - Ad-hoc conversions
  follow  `as_`, `to_`, `into_` conventions
- [ ] [C-GETTER](https://rust-lang.github.io/api-guidelines/naming.html#getter-names-follow-rust-convention-c-getter) - Getter names follow Rust convention
- [ ] [C-COMMON-TRAITS](https://rust-lang.github.io/api-guidelines/interoperability.html#c-common-traits) - Types eagerly implement common traits
  - `Copy`, `Clone`, `Eq`, `PartialEq`, `Ord`, `PartialOrd`, `Hash`, `Default`, `Debug`
  - `Display` where type wants to be displayed
- [ ] [C-CTOR](https://rust-lang.github.io/api-guidelines/predictability.html?highlight=new#constructors-are-static-inherent-methods-c-ctor) -
  Constructors are static, inherent methods
  - In particular, have `Foo::new()`, even if you have `Foo::default()`
- [ ] [C-FEATURE](https://rust-lang.github.io/api-guidelines/naming.html#feature-names-are-free-of-placeholder-words-c-feature) - Feature names
  are free of placeholder words


---


# Libraries / Building Guidelines



## Features are Additive  (M-FEATURES-ADDITIVE) { #M-FEATURES-ADDITIVE }

<why>To prevent compilation breakage in large and complex projects.</why>
<version>1.0</version>

All library features must be additive, and any combination must work, as long as the feature itself would work on the current platform. This implies:

- [ ] You must not introduce a `no-std` feature, use a `std` feature instead
- [ ] Adding any feature `foo` must not disable or modify any public item
  - Adding enum variants is fine if these enums are `#[non_exhaustive]`
- [ ] Features must not rely on other features to be manually enabled
- [ ] Features must not rely on their parent to skip-enable a feature in one of their children

Further Reading

- [Feature Unification](https://doc.rust-lang.org/cargo/reference/features.html#feature-unification)
- [Mutually Exclusive Features](https://doc.rust-lang.org/cargo/reference/features.html#mutually-exclusive-features)



## Libraries Work Out of the Box (M-OOBE) { #M-OOBE }

<why>To be easily adoptable by the Rust ecosystem.</why>
<version>1.0</version>

Libraries must _just work_ on all supported platforms, with the exception of libraries that are expressly platform or target specific.

Rust crates often come with dozens of dependencies, applications with 100's. Users expect `cargo build` and `cargo install`
to _just work_. Consider this installation of `bat` that pulls in ~250 dependencies:

```text
Compiling writeable v0.5.5
Compiling strsim v0.11.1
Compiling litemap v0.7.5
Compiling crossbeam-utils v0.8.21
Compiling icu_properties_data v1.5.1
Compiling ident_case v1.0.1
Compiling once_cell v1.21.3
Compiling icu_normalizer_data v1.5.1
Compiling fnv v1.0.7
Compiling regex-syntax v0.8.5
Compiling anstyle v1.0.10
Compiling vcpkg v0.2.15
Compiling utf8parse v0.2.2
Compiling aho-corasick v1.1.3
Compiling utf16_iter v1.0.5
Compiling hashbrown v0.15.2
Building [==>                       ] 29/251: icu_locid_transform_data, serde, winnow, indexma...
```

This compilation, like practically all other applications and libraries, will _just work_.

While there are tools targeting specific functionality (e.g., a Wayland compositor) or platform crates like
`windows`; unless a crate is _obviously_ platform specific, the expectation is that it will otherwise _just work_.

This means crates must build, ultimately

- [ ] on all [Tier 1 platforms](https://doc.rust-lang.org/rustc/platform-support.html),<sup>1</sup> and
- [ ] without any additional prerequisites beyond `cargo` and `rust`.<sup>2</sup>

<footnotes>

<sup>1</sup> It is ok to not support Tier 1 platforms "for now", but abstractions must be present so support can easily be extended. This is usually
done by introducing an internal `HAL` ([Hardware Abstraction Layer](https://en.wikipedia.org/wiki/HAL_(software))) module with a `dummy` fallback target.<br/>
<sup>2</sup> A default Rust installation will also have `cc` and a linker present.

</footnotes>

In particular, non-platform crates must not, by default, require the user to install additional tools, or expect environment variables
to compile. If tools were somehow needed (like the generation of Rust from `.proto` files) these tools should be run as part of the
publishing workflow or earlier, and the resulting artifacts (e.g., `.rs` files) be contained inside the published crate.

If a dependency is known to be platform specific, the parent must use conditional (platform) compilation or opt-in feature gates.

> **<alert></alert> Libraries are Responsible for Their Dependencies.**
>
> Imagine you author a `Copilot` crate, which in turn uses an `HttpClient`, which in turn depends on a `perl` script to compile.
>
> Then every one of your users, and your user's users, and everyone above, would need to install Perl to compile _their_ crate. In large projects you would
> have 100's of people who don't know or don't care about your library or Perl, encounter a cryptic compilation error, and now have to figure out how to
> install it on their system.
>
> In practical terms, such behavior is largely a self-inflicted death sentence in the open source space, since the moment alternatives
> are available, people will switch to those that _just work_.



## Native `-sys` Crates Compile Without Dependencies (M-SYS-CRATES) { #M-SYS-CRATES }

<why>To have libraries that 'just work' on all platforms.</why>
<version>0.2</version>

If you author a pair of `foo` and `foo-sys` crates wrapping a native `foo.lib`, you are likely to run into the issues described
in [M-OOBE].

Follow these steps to produce a crate that _just works_ across platforms:

- [ ] fully govern the build of `foo.lib` from `build.rs` inside `foo-sys`. Only use hand-crafted compilation via the
  [cc](https://crates.io/crates/cc) crate, do _not_ run Makefiles or external build scripts, as that will require the installation of external dependencies,
- [ ] make all external tools optional, such as `nasm`,
- [ ] embed the upstream source code in your crate,
- [ ] make the embedded sources verifiable (e.g., include Git URL + hash),
- [ ] pre-generate `bindgen` glue if possible,
- [ ] support both static linking, and dynamic linking via [libloading](https://crates.io/crates/libloading).

Deviations from these points can work, and can be considered on a case-by-case basis:

If the native build system is available as an _OOBE_ crate, that can be used instead of `cc` invocations. The same applies to external tools.

Source code might have to be downloaded if it does not fit crates.io size limitations. In any case, only servers with an availability
comparable to crates.io should be used. In addition, the specific hashes of acceptable downloads should be stored in the crate and verified.

Downloading sources can fail on hermetic build environments, therefore alternative source roots should also be specifiable (e.g., via environment variables).

[M-OOBE]: ./#M-OOBE


---


# Libraries / Interoperability Guidelines



## Don't Leak External Types (M-DONT-LEAK-TYPES) { #M-DONT-LEAK-TYPES }

<why>To prevent accidental breakage and long-term maintenance cost.</why>
<version>0.1</version>

Where possible, you should prefer `std`<sup>1</sup> types in public APIs over types coming from external crates. Exceptions should be carefully considered.

Any type in any public API will become part of that API's contract. Since `std` and constituents are the only crates
shipped by default, and since they come with a permanent stability guarantee, their types are the only ones that come without an interoperability risk.

A crate that exposes another crate's type is said to _leak_ that type.

For maximal long term stability your crate should, theoretically, not leak any types. Practically, some leakage
is unavoidable, sometimes even beneficial. We recommend you follow this heuristic:

- [ ] if you can avoid it, do not leak third-party types
- [ ] if you are part of an umbrella crate,<sup>2</sup> you may freely leak types from sibling crates.
- [ ] behind a relevant feature flag, types may be leaked (e.g., `serde`)
- [ ] without a feature _only_ if they give a _substantial benefit_. Most commonly that is interoperability with significant
      other parts of the Rust ecosystem based around these types.

<footnotes>

<sup>1</sup> In rare instances, e.g., high performance libraries used from embedded, you might even want to limit yourself to `core` only.

<sup>2</sup> For example, a `runtime` crate might be the umbrella of `runtime_rt`, `runtime_app` and `runtime_clock` As users are
expected to only interact with the umbrella, siblings may leak each others types.

</footnotes>



## Native Escape Hatches (M-ESCAPE-HATCHES) { #M-ESCAPE-HATCHES }

<why>To allow users to work around unsupported use cases until alternatives are available.</why>
<version>0.1</version>

Types wrapping native handles should provide `unsafe` escape hatches. In interop scenarios your users might have gotten a native handle from somewhere
else, or they might have to pass your wrapped handle over FFI. To enable these use cases you should provide `unsafe` conversion methods.

```rust
# type HNATIVE = *const u8;
pub struct Handle(HNATIVE);

impl Handle {
    pub fn new() -> Self {
        // Safely creates handle via API calls
        # todo!()
    }

    // Constructs a new Handle from a native handle the user got elsewhere.
    // This method  should then also document all safety requirements that
    // must be fulfilled.
    pub unsafe fn from_native(native: HNATIVE) -> Self {
        Self(native)
    }

    // Various extra methods to permanently or temporarily obtain
    // a native handle.
    pub fn into_native(self) -> HNATIVE { self.0 }
    pub fn to_native(&self) -> HNATIVE { self.0 }
}
```



## Types are Send (M-TYPES-SEND) { #M-TYPES-SEND }

<why>To enable the use of types in Tokio and behind runtime abstractions</why>
<version>1.0</version>

Public types should be `Send` for compatibility reasons:

- All futures produced (explicitly or implicitly) must be `Send`
- Most other types should be `Send`, but there might be exceptions

### Futures

When declaring a future explicitly you should ensure it is, and remains, `Send`.

```rust
# use std::future::Future;
# use std::pin::Pin;
# use std::task::{Context, Poll};
#
struct Foo {}

impl Future for Foo {
    // Explicit implementation of `Future` for your type
    # type Output = ();
    #
    # fn poll(self: Pin<&mut Self>, _: &mut Context<'_>) -> Poll<<Self as Future>::Output> { todo!() }
}

// You should assert your type is `Send`
const fn assert_send<T: Send>() {}
const _: () = assert_send::<Foo>();
```

When returning futures implicitly through `async` method calls, you should make sure these are `Send` too.
You do not have to test every single method, but you should at least validate your main entry points.

```rust,edition2021
async fn foo() { }

// TODO: We want this as a macro as well
fn assert_send<T: Send>(_: T) {}
_ = assert_send(foo());
```

### Regular Types

Most regular types should be `Send`, as they otherwise infect futures turning them `!Send` if held across `.await` points.

```rust,edition2021
# use std::rc::Rc;
# async fn read_file(x: &str) {}
#
async fn foo() {
    let rc = Rc::new(123);      // <-- Holding this across an .await point prevents
    read_file("foo.txt").await; //     the future from being `Send`.
    dbg!(rc);
}
```

That said, if the default use of your type is _instantaneous_, and there is no reason for it to be otherwise held across `.await` boundaries, it may be `!Send`.

```rust,edition2021
# use std::rc::Rc;
# struct Telemetry; impl Telemetry { fn ping(&self, _: u32) {} }
# fn telemetry() -> Telemetry  { Telemetry }
# async fn read_file(x: &str) {}
#
async fn foo() {
    // Here a hypothetical instance Telemetry is summoned
    // and used ad-hoc. It may be ok for Telemetry to be !Send.
    telemetry().ping(0);
    read_file("foo.txt").await;
    telemetry().ping(1);
}
```

> ### <tip></tip> The Cost of Send
>
> Ideally, there would be abstractions that are `Send` in work-stealing runtimes, and `!Send` in thread-per-core models based on non-atomic
> types like `Rc` and `RefCell` instead.
>
> Practically these abstractions don't exist, preventing Tokio compatibility in the non-atomic case. That in turn means you would have to
> "reinvent the world" to get anything done in a thread-per-core universe.
>
> The good news is, in most cases atomics and uncontended locks only have a measurable impact if accessed more frequently than every 64 words or so.
>
> <div style="background-color:white;">
>
> ![TEXT](M-TYPES-SEND.png)
>
> </div>
>
> Working with a large `Vec<AtomicUsize>` in a hot loop is a bad idea, but doing the occasional uncontended atomic operation from otherwise thread-per-core
> async code has no performance impact, but gives you widespread ecosystem compatibility.


---


# Libraries / Resilience Guidelines



## Avoid Statics (M-AVOID-STATICS) { #M-AVOID-STATICS }

<why>To prevent consistency and correctness issues between crate versions.</why>
<version>1.0</version>

Libraries should avoid `static` and thread-local items, if a consistent view of the item is relevant for correctness.
Essentially, any code that would be incorrect if the static _magically_ had another value must not use them. Statics
only used for performance optimizations are ok.

The fundamental issue with statics in Rust is the secret duplication of state.

Consider a crate `core` with the following function:

```rust
# use std::sync::atomic::AtomicUsize;
# use std::sync::atomic::Ordering;
static GLOBAL_COUNTER: AtomicUsize = AtomicUsize::new(0);

pub fn increase_counter() -> usize {
    GLOBAL_COUNTER.fetch_add(1, Ordering::Relaxed)
}
```

Now assume you have a crate `main`, calling two libraries `library_a` and `library_b`, each invoking that counter:

```rust,ignore
// Increase global static counter 2 times
library_a::count_up();
library_a::count_up();

// Increase global static counter 3 more times
library_b::count_up();
library_b::count_up();
library_b::count_up();
```

They eventually report their result:

```rust,ignore
library_a::print_counter();
library_b::print_counter();
main::print_counter();
```

At this point, what is _the_ value of said counter; `0`, `2`, `3` or `5`?

The answer is, possibly any  (even multiple!) of the above, depending on the crate's version resolution!

Under the hood Rust may link to multiple versions of the same crate, independently instantiated, to satisfy declared
dependencies. This is especially observable during a crate's `0.x` version timeline, where each `x` constitutes a separate _major_ version.

If `main`,  `library_a` and `library_b` all declared the same version of `core`, e.g. `0.5`, then the reported result will be `5`, since all
crates actually _see_ the same version of `GLOBAL_COUNTER`.

However, if `library_a` declared `0.4` instead, then it would be linked against a separate version of `core`; thus `main` and `library_b` would
agree on a value of `3`, while `library_a` reported `2`.

Although `static` items can be useful, they are particularly dangerous before a library's stabilization, and for any state where _secret duplication_ would
cause consistency issues when static and non-static variable use interacts. In addition, statics interfere with unit testing, and are a contention point in
thread-per-core designs.



## I/O and System Calls Are Mockable (M-MOCKABLE-SYSCALLS) { #M-MOCKABLE-SYSCALLS }

<why>To make otherwise hard-to-evoke edge cases testable.</why>
<version>0.2</version>

Any user-facing type doing I/O, or sys calls with side effects, should be mockable to these effects. This includes file and
network access, clocks, entropy sources and seeds, and similar. More generally, any operation that is

- non-deterministic,
- reliant on external state,
- depending on the hardware or the environment,
- is otherwise fragile or not universally reproducible

should be mockable.

> ### <tip></tip> Mocking Allocations?
>
> Unless you write kernel code or similar, you can consider allocations to be deterministic, hardware independent and practically
> infallible, thus not covered by this guideline.
>
> However, this does _not_ mean you should expect there to be unlimited memory available. While it is ok to
> accept caller provided input as-is if your library has a _reasonable_ memory complexity, memory-hungry libraries
> and code handling external input should provide bounded and / or chunking operations.

This guideline has several implications for libraries, they

- should not perform ad-hoc I/O, i.e., call `read("foo.txt")`
- should not rely on non-mockable I/O and sys calls
- should not create their own I/O or sys call _core_ themselves
- should not offer `MyIoLibrary::default()` constructors

Instead, libraries performing I/O and sys calls should either accept some I/O _core_ that is mockable already, or provide mocking functionality themselves:

```rust, ignore
let lib = Library::new_runtime(runtime_io); // mockable I/O functionality passed in
let (lib, mock) = Library::new_mocked(); // supports inherent mocking
```

Libraries supporting inherent mocking should implement it as follows:

```rust, ignore
pub struct Library {
    some_core: LibraryCore // Encapsulates syscalls, I/O, ... compare below.
}

impl Library {
    pub fn new() -> Self { ... }
    pub fn new_mocked() -> (Self, MockCtrl) { ... }
}
```

Behind the scenes, `LibraryCore` is a non-public enum, similar to [M-RUNTIME-ABSTRACTED], that either dispatches
calls to the respective sys call, or to an mocking controller.

```rust, ignore
// Dispatches calls either to the operating system, or to a
// mocking controller.
enum LibraryCore {
    Native,

    #[cfg(feature = "test-util")]
    Mocked(mock::MockCtrl)
}

impl LibraryCore {
    // Some function you'd forward to the operating system.
    fn random_u32(&self) {
        match self {
            Self::Native => unsafe { os_random_u32() }
            Self::Mocked(m) => m.random_u32()
        }
    }
}


#[cfg(feature = "test-util")]
mod mock {
    // This follows the M-SERVICES-CLONE pattern, so both `LibraryCore` and
    // the user can hold on to the same `MockCtrl` instance.
    pub struct MockCtrl {
        inner: Arc<MockCtrlInner>
    }

    // Implement required logic accordingly, usually forwarding to
    // `MockCtrlInner` below.
    impl MockCtrl {
        pub fn set_next_u32(&self, x: u32) { ... }
        pub fn random_u32(&self) { ... }
    }

    // Contains actual logic, e.g., the next random number we should return.
    struct MockCtrlInner {
        next_call: u32
    }
}
```

Runtime-aware libraries already build on top of the [M-RUNTIME-ABSTRACTED] pattern should extend their runtime enum instead:

```rust, ignore
enum Runtime {
    #[cfg(feature="tokio")]
    Tokio(tokio::Tokio),

    #[cfg(feature="smol")]
    Smol(smol::Smol)

    #[cfg(feature="test-util")]
    Mock(mock::MockCtrl)
}
```

As indicated above, most libraries supporting mocking should not accept mock controllers, but return them via parameter tuples,
with the first parameter being the library instance, the second the mock controller. This is to prevent state ambiguity if multiple
instances shared a single controller:

```rust, ignore
impl Library {
    pub fn new_mocked() -> (Self, MockCtrl) { ... } // good
    pub fn new_mocked_bad(&mut MockCtrl) -> Self { ... } // prone to misuse
}
```

[M-RUNTIME-ABSTRACTED]: ../ux/#M-RUNTIME-ABSTRACTED



## Don't Glob Re-Export Items (M-NO-GLOB-REEXPORTS) { #M-NO-GLOB-REEXPORTS }

<why>To prevent accidentally leaking unintended types.</why>
<version>1.0</version>

Don't `pub use foo::*` from other modules, especially not from other crates. You might accidentally export more than you want,
and globs are hard to review in PRs. Re-export items individually instead:

```rust,ignore
pub use foo::{A, B, C};
```

Glob exports are permissible for technical reasons, like doing platform specific re-exports from a set of HAL (hardware abstraction layer) modules:

```rust,ignore
#[cfg(target_os = "windows")]
mod windows { /* ... */ }

#[cfg(target_os = "linux")]
mod linux { /* ... */ }

// Acceptable use of glob re-exports, this is a common pattern
// and it is clear everything is just forwarded from a single 
// platform.

#[cfg(target_os = "windows")]
pub use windows::*;

#[cfg(target_os = "linux")]
pub use linux::*;
```



## Use the Proper Type Family (M-STRONG-TYPES) { #M-STRONG-TYPES }

<why>To have and maintain the right data and safety variants, at the right time.</why>
<version>1.0</version>

Use the appropriate `std` type for your task. In general you should use the strongest type available, as early as possible in your API flow. Common offenders are

| Do not use ... | use instead ... | Explanation |
| --- | --- | --- |
| `String`* | `PathBuf`* | Anything dealing with the OS should be `Path`-like |

That said, you should also follow common Rust `std` conventions. Purely numeric types at public API boundaries (e.g., `window_size()`) are expected to
be regular numbers, not `Saturating<usize>`, `NonZero<usize>`, or similar.

<footnotes>

<sup>*</sup> Including their siblings, e.g., `&str`, `Path`, ...

</footnotes>



## Test Utilities are Feature Gated (M-TEST-UTIL) { #M-TEST-UTIL }

<why>To prevent production builds from accidentally bypassing safety checks.</why>
<version>0.2</version>

Testing functionality must be guarded behind a feature flag. This includes

- mocking functionality ([M-MOCKABLE-SYSCALLS]),
- the ability to inspect sensitive data,
- safety check overrides,
- fake data generation.

We recommend you use a single flag only, named `test-util`. In any case, the feature(s) must clearly communicate they are for testing purposes.

```rust, ignore
impl HttpClient {
    pub fn get() { ... }

    #[cfg(feature = "test-util")]
    pub fn bypass_certificate_checks() { ... }
}
```

[M-MOCKABLE-SYSCALLS]: ./#M-MOCKABLE-SYSCALLS


---


# Libraries / UX Guidelines



## Avoid Smart Pointers and Wrappers in APIs (M-AVOID-WRAPPERS) { #M-AVOID-WRAPPERS }

<why>To reduce cognitive load and improve API ergonomics.</why>
<version>1.0</version>

As a specialization of [M-ABSTRACTIONS-DONT-NEST], generic wrappers and smart pointers like
`Rc<T>`, `Arc<T>`, `Box<T>`, or `RefCell<T>` should be avoided in public APIs.

From a user perspective these are mostly implementation details, and introduce infectious complexity that users have to
resolve. In fact, these might even be impossible to resolve once multiple crates disagree about the required type of wrapper.

If wrappers are needed internally, they should be hidden behind a clean API that uses simple types like `&T`, `&mut T`, or `T` directly. Compare:

```rust,ignore
// Good: simple API
pub fn process_data(data: &Data) -> State { ... }
pub fn store_config(config: Config) -> Result<(), Error> { ... }

// Bad: Exposing implementation details
pub fn process_shared(data: Arc<Mutex<Shared>>) -> Box<Processed> { ... }
pub fn initialize(config: Rc<RefCell<Config>>) -> Arc<Server> { ... }
```

Smart pointers in APIs are acceptable when:

- The smart pointer is fundamental to the API's purpose (e.g., a new container lib)

- The smart pointer, based on benchmarks, significantly improves performance and the complexity is justified.

[M-ABSTRACTIONS-DONT-NEST]: ./#M-ABSTRACTIONS-DONT-NEST



## Prefer Types over Generics, Generics over Dyn Traits (M-DI-HIERARCHY) { #M-DI-HIERARCHY }

<why>To prevent patterns that don't compose, and design lock-in.</why>
<version>0.1</version>

When asking for async dependencies, prefer concrete types over generics, and generics over `dyn Trait`.

It is easy to accidentally deviate from this pattern when porting code from languages like C# that heavily rely on interfaces.
Consider you are porting a service called `Database` from C# to Rust and, inspired by the original `IDatabase` interface, you naively translate it into:

```rust,ignore
trait Database {
    async fn update_config(&self, file: PathBuf);
    async fn store_object(&self, id: Id, obj: Object);
    async fn load_object(&self, id: Id) -> Object;
}

impl Database for MyDatabase { ... }

// Intended to be used like this:
async fn start_service(b: Rc<dyn Database>) { ... }
```

Apart from not feeling idiomatic, this approach precludes other Rust constructs that conflict with object safety,
can cause issues with asynchronous code, and exposes wrappers (compare [M-AVOID-WRAPPERS]).

Instead, when more than one implementation is needed, this _design escalation ladder_ should be followed:

If the other implementation is only concerned with providing a _sans-io_ implementation for testing, implement your type as an
enum, following [M-MOCKABLE-SYSCALLS] instead.

If users are expected to provide custom implementations, you should introduce one or more traits, and implement them for your own types
_on top_ of your inherent functions. Each trait should be relatively narrow, e.g., `StoreObject`, `LoadObject`. If eventually a single
trait is needed it should be made a subtrait, e.g., `trait DataAccess: StoreObject + LoadObject {}`.

Code working with these traits should ideally accept them as generic type parameters as long as their use does not contribute to significant nesting
(compare [M-ABSTRACTIONS-DONT-NEST]).

```rust,ignore
// Good, generic does not have infectious impact, uses only most specific trait
async fn read_database(x: impl LoadObject) { ... }

// Acceptable, unless further nesting makes this excessive.
struct MyService<T: DataAccess> {
    db: T,
}
```

Once generics become a nesting problem, `dyn Trait` can be considered. Even in this case, visible wrapping should be avoided, and custom wrappers should be preferred.

```rust
# use std::sync::Arc;
# trait DataAccess {
#     fn foo(&self);
# }
// This allows you to expand or change `DynamicDataAccess` later. You can also
// implement `DataAccess` for `DynamicDataAccess` if needed, and use it with
// regular generic functions.
struct DynamicDataAccess(Arc<dyn DataAccess>);

impl DynamicDataAccess {
    fn new<T: DataAccess + 'static>(db: T) -> Self {
        Self(Arc::new(db))
    }
}

struct MyService {
    db: DynamicDataAccess,
}
```

The generic wrapper can also be combined with the enum approach from [M-MOCKABLE-SYSCALLS]:

```rust,ignore
enum DataAccess {
    MyDatabase(MyDatabase),
    Mock(mock::MockCtrl),
    Dynamic(DynamicDataAccess)
}

async fn read_database(x: &DataAccess) { ... }
```

[M-AVOID-WRAPPERS]: ./#M-AVOID-WRAPPERS
[M-MOCKABLE-SYSCALLS]: ../resilience/#M-MOCKABLE-SYSCALLS
[M-ABSTRACTIONS-DONT-NEST]: ./#M-ABSTRACTIONS-DONT-NEST



## Error are Canonical Structs (M-ERRORS-CANONICAL-STRUCTS) { #M-ERRORS-CANONICAL-STRUCTS }

<why>To harmonize the behavior of error types, and provide a consistent error handling.</why>
<version>1.0</version>

Errors should be a situation-specific `struct` that contain a [`Backtrace`](https://doc.rust-lang.org/stable/std/backtrace/struct.Backtrace.html),
a possible upstream error cause, and helper methods.

Simple crates usually expose a single error type `Error`, complex crates may expose multiple types, for example
`AccessError` and `ConfigurationError`. Error types should provide helper methods for additional information that allows callers to handle the error.

A simple error might look like so:

```rust
# use std::backtrace::Backtrace;
# use std::fmt::Display;
# use std::fmt::Formatter;
pub struct ConfigurationError {
    backtrace: Backtrace,
}

impl ConfigurationError {
    pub(crate) fn new() -> Self {
        Self { backtrace: Backtrace::capture() }
    }
}

// Impl Debug + Display
```

Where appropriate, error types should provide contextual error information, for example:

```rust,ignore
# use std::backtrace::Backtrace;
# #[derive(Debug)]
# pub struct ConfigurationError {
#    backtrace: Backtrace,
# }
impl ConfigurationError {
    pub fn config_file(&self) -> &Path { }
}
```

If your API does mixed operations, or depends on various upstream libraries, store an `ErrorKind`.
Error kinds, and more generally enum-based errors, should not be used to avoid creating separate public error types when there is otherwise no error overlap:

```rust, ignore
// Prefer this
fn download_iso() -> Result<(), DownloadError> {}
fn start_vm() -> Result<(), VmError> {}

// Over that
fn download_iso() -> Result<(), GlobalEverythingErrorEnum> {}
fn start_vm() -> Result<(), GlobalEverythingErrorEnum> {}

// However, not every function warrants a new error type. Errors
// should be general enough to be reused.
fn parse_json() -> Result<(), ParseError> {}
fn parse_toml() -> Result<(), ParseError> {}
```

If you do use an inner `ErrorKind`, that enum should not be exposed directly for future-proofing reasons,
as otherwise you would expose your callers to _all_ possible failure modes, even the ones you consider internal
and unhandleable. Instead, expose various `is_xxx()` methods as shown below:

```rust
# use std::backtrace::Backtrace;
# use std::fmt::Display;
# use std::fmt::Formatter;
#[derive(Debug)]
pub(crate) enum ErrorKind {
    Io(std::io::Error),
    Protocol
}

#[derive(Debug)]
pub struct HttpError {
    kind: ErrorKind,
    backtrace: Backtrace,
}

impl HttpError {
    pub fn is_io(&self) -> bool { matches!(self.kind, ErrorKind::Io(_)) }
    pub fn is_protocol(&self) -> bool { matches!(self.kind, ErrorKind::Protocol) }
}
```

Most upstream errors don't provide a backtrace. You should capture one when creating an `Error` instance, either via one of
your `Error::new()` flavors, or when implementing `From<UpstreamError> for Error {}`.

Error structs must properly implement `Display` that renders as follows:

```rust,ignore
impl Display for MyError {
    // Print a summary sentence what happened.
    // Print `self.backtrace`.
    // Print any additional upstream 'cause' information you might have.
#   fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
#       todo!()
#   }
}
```

Errors must also implement `std::error::Error`:

```rust,ignore
impl std::error::Error for MyError { }
```

Lastly, if you happen to emit lots of errors from your crate, consider creating a private `bail!()` helper macro to simplify error instantiation.

> ### <tip></tip> When You Get Backtraces
>
> Backtraces are an invaluable debug tool in complex or async code, since  errors might _travel_ far through a callstack before being surfaced.
>
> That said, they are a _development_ tool, not a _runtime_ diagnostic, and by default `Backtrace::capture()` will **not** capture
> backtraces, as they have a large overhead, e.g., 4μs per capture on the author's PC.
>
> Instead, Rust evaluates a [set of environment variables](https://doc.rust-lang.org/stable/std/backtrace/index.html#environment-variables), such as
> `RUST_BACKTRACE`, and only walks the call frame when explicitly asked. Otherwise it captures an empty trace, at the cost of only a few CPU instructions.



## Essential Functionality Should be Inherent (M-ESSENTIAL-FN-INHERENT) { #M-ESSENTIAL-FN-INHERENT }

<why>To make essential functionality easily discoverable.</why>
<version>1.0</version>

Types should implement core functionality inherently. Trait implementations should forward to inherent functions, and not replace them. Instead of this

```rust
# trait Download {
#     fn download_file(&self, url: impl AsRef<str>);
# }
struct HttpClient {}

// Offloading essential functionality into traits means users
// will have to figure out what other traits to `use` to
// actually use this type.
impl Download for HttpClient {
    fn download_file(&self, url: impl AsRef<str>) {
        // ... logic to download a file
    }
}
```

do this:

```rust
# trait Download {
#     fn download_file(&self, url: impl AsRef<str>);
# }
struct HttpClient {}

impl HttpClient {
    fn download_file(&self, url: impl AsRef<str>) {
        // ... logic to download a file
    }
}

// Forward calls to inherent impls. `HttpClient` can be used
impl Download for HttpClient {
    fn download_file(&self, url: impl AsRef<str>) {
        Self::download_file(self, url)
    }
}
```



## Accept `impl AsRef<>` Where Feasible (M-IMPL-ASREF) { #M-IMPL-ASREF }

<why>To give users flexibility calling in with their own types.</why>
<version>1.0</version>

In **function** signatures, accept `impl AsRef<T>` for types that have a
[clear reference hierarchy](https://doc.rust-lang.org/stable/std/convert/trait.AsRef.html#implementors), where you
do not need to take ownership, or where object creation is relatively cheap.

| Instead of ... | accept ... |
| --- | --- |
| `&str`, `String` | `impl AsRef<str>` |
| `&Path`, `PathBuf` | `impl AsRef<Path>` |
| `&[u8]`, `Vec<u8>` | `impl AsRef<[u8]>` |

```rust,ignore
# use std::path::Path;
// Definitely use `AsRef`, the function does not need ownership.
fn print(x: impl AsRef<str>) {}
fn read_file(x: impl AsRef<Path>) {}
fn send_network(x: impl AsRef<[u8]>) {}

// Further analysis needed. In these cases the function wants
// ownership of some `String` or `Vec<u8>`. If those are
// "low freqency, low volume" functions `AsRef` has better ergonomics,
// otherwise accepting a `String` or `Vec<u8>` will have better
// performance.
fn new_instance(x: impl AsRef<str>) -> HoldsString {}
fn send_to_other_thread(x: impl AsRef<[u8]>) {}
```

In contrast, **types** should generally not be infected by these bounds:

```rust,ignore
// Generally not ok. There might be exceptions for performance
// reasons, but those should not be user visible.
struct User<T: AsRef<str>> {
    name: T
}

// Better
struct User {
    name: String
}
```



## Accept `impl 'IO'` Where Feasible ('Sans IO') (M-IMPL-IO) { #M-IMPL-IO }

<why>To untangle business logic from I/O logic, and have N*M composability.</why>
<version>0.1</version>

Functions and types that only need to perform one-shot I/O during initialization should be written "[sans-io](https://www.firezone.dev/blog/sans-io)",
and accept some `impl T`, where `T` is the appropriate I/O trait, effectively outsourcing I/O work to another type:

```rust,ignore
// Bad, caller must provide a File to parse the given data. If this
// data comes from the network, it'd have to be written to disk first.
fn parse_data(file: File) {}
```

```rust
// Much better, accepts
// - Files,
// - TcpStreams,
// - Stdin,
// - &[u8],
// - UnixStreams,
// ... and many more.
fn parse_data(data: impl std::io::Read) {}
```

Synchronous functions should use [`std::io::Read`](https://doc.rust-lang.org/std/io/trait.Read.html) and
[`std::io::Write`](https://doc.rust-lang.org/std/io/trait.Write.html). Asynchronous _functions_ targeting more than one runtime should use
[`futures::io::AsyncRead`](https://docs.rs/futures/latest/futures/io/trait.AsyncRead.html) and similar.
_Types_ that need to perform runtime-specific, continuous I/O should follow [M-RUNTIME-ABSTRACTED] instead.

[M-RUNTIME-ABSTRACTED]: ./#M-RUNTIME-ABSTRACTED



## Accept `impl RangeBounds<>` Where Feasible (M-IMPL-RANGEBOUNDS) { #M-IMPL-RANGEBOUNDS }

<why>To give users flexibility and clarity when specifying ranges.</why>
<version>1.0</version>

Functions that accept a range of numbers must use a `Range` type or trait over hand-rolled parameters:

```rust,ignore
// Bad
fn select_range(low: usize, high: usize) {}
fn select_range(range: (usize, usize)) {}
```

In addition, functions that can work on arbitrary ranges, should accept `impl RangeBounds<T>` rather than `Range<T>`.

```rust
# use std::ops::{RangeBounds, Range};
// Callers must call with `select_range(1..3)`
fn select_range(r: Range<usize>) {}

// Callers may call as
//     select_any(1..3)
//     select_any(1..)
//     select_any(..)
fn select_any(r: impl RangeBounds<usize>) {}
```



## Complex Type Construction has Builders (M-INIT-BUILDER) { #M-INIT-BUILDER }

<why>To future-proof type construction in complex scenarios.</why>
<version>0.3</version>

Types that could support 4 or more arbitrary initialization permutations should provide builders. In other words, types with up to
2 optional initialization parameters can be constructed via inherent methods:

```rust
# struct A;
# struct B;
struct Foo;

// Supports 2 optional construction parameters, inherent methods ok.
impl Foo {
    pub fn new() -> Self { Self }
    pub fn with_a(a: A) -> Self { Self }
    pub fn with_b(b: B) -> Self { Self }
    pub fn with_a_b(a: A, b: B) -> Self { Self }
}
```

Beyond that, types should provide a builder:

```rust, ignore
# struct A;
# struct B;
# struct C;
# struct Foo;
# struct FooBuilder;
impl Foo {
    pub fn new() -> Self { ... }
    pub fn builder() -> FooBuilder { ... }
}

impl FooBuilder {
    pub fn a(mut self, a: A) -> Self { ... }
    pub fn b(mut self, b: B) -> Self { ... }
    pub fn c(mut self, c: C) -> Self { ... }
    pub fn build(self) -> Foo { ... }
}

```

The proper name for a builder that builds `Foo` is `FooBuilder`. Its methods must be chainable, with the final method called
`.build()`. The buildable struct must have a shortcut `Foo::builder()`, while the builder itself should _not_ have a public
`FooBuilder::new()`. Builder methods that set a value `x` are called `x()`, not `set_x()` or similar.

### Builders and Required Parameters

Required parameters should be passed when creating the builder, not as setter methods. For builders with multiple required
parameters, encapsulate them into a parameters struct and use the `deps: impl Into<Deps>` pattern to provide flexibility:

> **Note:** A dedicated deps struct is not required if the builder has no required parameters or only a single simple parameter. However,
> for backward compatibility and API evolution, it's preferable to use a dedicated struct for deps even in simple cases, as it makes it
> easier to add new required parameters in the future without breaking existing code.

```rust, ignore
#[derive(Debug, Clone)]
pub struct FooDeps {
    pub logger: Logger,
    pub config: Config,
}

impl From<(Logger, Config)> for FooDeps { ... }
impl From<Logger> for FooDeps { ... } // In case we could use default Config instance

impl Foo {
    pub fn builder(deps: impl Into<FooDeps>) -> FooBuilder { ... }
}
```

This pattern allows for convenient usage:

- `Foo::builder(logger)` - when only the logger is needed
- `Foo::builder((logger, config))` - when both parameters are needed
- `Foo::builder(FooDeps { logger, config })` - explicit struct construction

Alternatively, you can use [`fundle`](https://docs.rs/fundle) to simplify the creation of `FooDeps`:

```rust, ignore
#[derive(Debug, Clone)]
#[fundle::deps]
pub struct FooDeps {
    pub logger: Logger,
    pub config: Config,
}
```

This pattern enables "dependency injection", see [these docs](https://docs.rs/fundle/latest/fundle/attr.deps.html) for more details.

### Runtime-Specific Builders

For types that are runtime-specific or require runtime-specific configuration, provide dedicated builder creation methods that accept the appropriate runtime parameters:

```rust, ignore
#[cfg(feature="smol")]
#[derive(Debug, Clone)]
pub struct SmolDeps {
    pub clock: Clock,
    pub io_context: Context,
}

#[cfg(feature="tokio")]
#[derive(Debug, Clone)]
pub struct TokioDeps {
    pub clock: Clock,
}

impl Foo {
    #[cfg(feature="smol")]
    pub fn builder_smol(deps: impl Into<SmolDeps>) -> FooBuilder { ... }

    #[cfg(feature="tokio")]
    pub fn builder_tokio(deps: impl Into<TokioDeps>) -> FooBuilder { ... }
}
```

This approach ensures type safety at compile time and makes the runtime dependency explicit in the API surface. The resulting
builder methods follow the pattern `builder_{runtime}(deps)` where `{runtime}` indicates the specific runtime or execution environment.

### Further Reading

- [Builder pattern in Rust: self vs. &mut self, and method vs. associated function](https://users.rust-lang.org/t/builder-pattern-in-rust-self-vs-mut-self-and-method-vs-associated-function/72892)
- [fundle](https://docs.rs/fundle)



## Complex Type Initialization Hierarchies are Cascaded (M-INIT-CASCADED) { #M-INIT-CASCADED }

<why>To prevent misuse and accidental parameter mix ups.</why>
<version>1.0</version>

Types that require 4+ parameters should cascade their initialization via helper types.

```rust, ignore
# struct Deposit;
impl Deposit {
    // Easy to confuse parameters and signature generally unwieldy.
    pub fn new(bank_name: &str, customer_name: &str, currency_name: &str, currency_amount: u64) -> Self { }
}
```

Instead of providing a long parameter list, parameters should be grouped semantically. When applying this guideline,
also check if [C-NEWTYPE] is applicable:

```rust, ignore
# struct Deposit;
# struct Account;
# struct Currency
impl Deposit {
    // Better, signature cleaner
    pub fn new(account: Account, amount: Currency) -> Self { }
}

impl Account {
    pub fn new_ok(bank: &str, customer: &str) -> Self { }
    pub fn new_even_better(bank: Bank, customer: Customer) -> Self { }
}
```

[C-NEWTYPE]: https://rust-lang.github.io/api-guidelines/type-safety.html#c-newtype



## Services are Clone (M-SERVICES-CLONE) { #M-SERVICES-CLONE }

<why>To avoid composability issues when sharing common services.</why>
<version>1.0</version>

Heavyweight _service_ types and 'thread singletons' should implement shared-ownership `Clone` semantics, including any type you expect to be used from your `Application::init`.

Per thread, users should essentially be able to create a single resource handler instance, and have it reused by other handlers on the same thread:

```rust,ignore
impl ThreadLocal for MyThreadState {
    fn init(...) -> Self {

        // Create common service instance possibly used by many.
        let common = ServiceCommon::new();

        // Users can freely pass `common` here multiple times
        let service_1 = ServiceA::new(&common);
        let service_2 = ServiceA::new(&common);

        Self { ... }
    }
}
```

Services then simply clone their dependency and store a new _handle_, as if `ServiceCommon` were a shared-ownership smart pointer:

```rust,ignore
impl ServiceA {
    pub fn new(common: &ServiceCommon) -> Self {
        // If we only need to access `common` from `new` we don't have
        // to store it. Otherwise, make a clone we store in `Self`.
        let common = common.clone();
    }
}
```

Under the hood this `Clone` should **not** create a fat copy of the entire service. Instead, it should follow the `Arc<Inner>` pattern:

```rust, ignore
// Actual service containing core logic and data.
struct ServiceCommonInner {}

#[derive(Clone)]
pub ServiceCommon {
    inner: Arc<ServiceCommonInner>
}

impl ServiceCommon {
    pub fn new() {
        Self { inner: Arc::new(ServiceCommonInner::new()) }
    }

    // Method forwards ...
    pub fn foo(&self) { self.inner.foo() }
    pub fn bar(&self) { self.inner.bar() }
}
```



## Abstractions Don't Visibly Nest (M-SIMPLE-ABSTRACTIONS) { #M-SIMPLE-ABSTRACTIONS }

<why>To prevent cognitive load and a bad out of the box UX.</why>
<version>0.1</version>

When designing your public types and primary API surface, avoid exposing nested or complex parametrized types to your users.

While powerful, type parameters introduce a cognitive load, even more so if the involved traits are crate-specific. Type parameters
become infectious to user code holding on to these types in their fields, often come with complex trait hierarchies on their own, and
might cause confusing error messages.

From the perspective of a user authoring `Foo`, where the other structs come from your crate:

```rust,ignore
struct Foo {
    service: Service // Great
    service: Service<Backend> // Acceptable
    service: Service<Backend<Store>> // Bad

    list: List<Rc<u32>> // Great, `List<T>` is simple container,
                        // other types user provided.

    matrix: Matrix4x4 // Great
    matrix: Matrix4x4<f32> // Still ok
    matrix: Matrix<f32, Const<4>, Const<4>, ArrayStorage<f32, 4, 4>> // ?!?
}
```

_Visible_ type parameters should be avoided in _service-like_ types (i.e., types mainly instantiated once per thread / application that are often passed
as dependencies), in particular if the nestee originates from the same crate as the service.

Containers, smart-pointers and similar data structures obviously must expose a type parameter, e.g., `List<T>` above. Even then, care should
be taken to limit the number and nesting of parameters.

To decide whether type parameter nesting should be avoided, consider these factors:

- Will the type be **named** by your users?
  - Service-level types are always expected to be named (e.g., `Library<T>`),
  - Utility types, such as the many [`std::iter`](https://doc.rust-lang.org/stable/std/iter/index.html) types like `Chain`, `Cloned`, `Cycle`, are not
    expected to be named.
- Does the type primarily compose with non-user types?
- Do the used type parameters have complex bounds?
- Do the used type parameters affect inference in other types or functions?

The more of these factors apply, the bigger the cognitive burden.

As a rule of thumb, primary service API types should not nest _on their own volition_, and if they do, only 1 level deep. In other words, these
APIs should not require users having to deal with an `Foo<Bar<FooBar>>`. However, if `Foo<T>` users want to bring their own `A<B<C>>` as `T` they
should be free to do so.

> ### <tip></tip> Type Magic for Better UX?
>
> The guideline above is written with 'bread-and-butter' types in mind you might create during  _normal_ development activity. Its intention is to
> reduce friction users encounter when working with your code.
>
> However, when designing API patterns and ecosystems at large, there might be valid reasons to introduce intricate type magic to overall _lower_
> the cognitive friction involved, [Bevy's ECS](https://docs.rs/bevy_ecs/latest/bevy_ecs/) or
> [Axum's request handlers](https://docs.rs/axum/latest/axum/handler/trait.Handler.html) come to mind.
>
> The threshold where this pays off is high though. If there is any doubt about the utility of your creative use of generics, your users might be
> better off without them.


---
