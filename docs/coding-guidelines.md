# Coding guidelines

## Comments

Every comment has to be justified by one of two reasons: the code is genuinely surprising, or it duplicates another place in a way that is not obvious and the two have to stay in sync. A comment that cannot be justified this way is cut. A comment describes what the code is, not what it was. Removed code is not mentioned in comments anymore.

## Code style

- Split an `if` with an init statement into two lines, `x, err := f()` first and the `if` after it.
- Choose between alternative sources with a tagless `switch`, not a chain of early returns.
- Feature flag checks and nil guards wrap the call, not the body of the called function.
- These rules apply to new lines. Existing code keeps its shape, even on lines a change touches.

## Performant code

- An `if` goes up. The caller decides whether a call happens, the called function does not open with a guard for it. With the conditions in one place, cases can be combined and handled together.

  ```go
  // good
  if opts.Cache {
      index := buildIndex(layers) // only built when it is pushed
      pushLayers(layers)
      pushIndex(index)
  }

  // bad
  func pushLayers(layers []v1.Layer) {
      if !opts.Cache {
          return
      }
      ...
  }
  func pushIndex(index v1.ImageIndex) {
      if !opts.Cache {
          return
      }
      ...
  }
  index := buildIndex(layers) // built even when nothing is pushed
  pushLayers(layers)
  pushIndex(index)
  ```
- A `for` goes down. A function takes the whole batch instead of being called in a loop. Processing the batch together can share setup costs that would otherwise be paid for every item.

  ```go
  // good
  func hashFiles(paths []string) {
      h := newHasher() // setup once
      for _, p := range paths {
          h.Hash(p)
      }
  }

  // bad
  func hashFile(p string) {
      h := newHasher() // setup for every file
      h.Hash(p)
  }
  for _, p := range paths {
      hashFile(p)
  }
  ```
