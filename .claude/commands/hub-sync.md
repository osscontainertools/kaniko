---
model: haiku
effort: low
---

Update `hub-sync.md` in the repo root with the new Docker Hub description for `martizih/kaniko`.

Usage: `/hub-sync`

1. **Get version and release date:**
   ```bash
   gh release list --repo osscontainertools/kaniko --limit 1 --json tagName,publishedAt \
     | jq -r '.[0] | .tagName, (.publishedAt | strptime("%Y-%m-%dT%H:%M:%SZ") | strftime("%d.%m.%Y"))'
   ```

2. **Get image digests** — convert `sha256:` → `sha256-` for URLs:
   ```bash
   crane digest martizih/kaniko:$VERSION
   crane digest martizih/kaniko:$VERSION-slim
   crane digest martizih/kaniko:$VERSION-debug
   crane digest martizih/kaniko:$VERSION-alpine
   crane digest martizih/kaniko:$VERSION-warmer
   crane digest martizih/kaniko:$VERSION-bootstrap
   ```

3. **Get current description:**
   ```bash
   curl -s https://hub.docker.com/v2/repositories/martizih/kaniko/ | jq -r '.full_description'
   ```

4. **Build updated description:**
   - `Supported Tags`: update sha256 links (`latest`→`$VERSION`, `slim`→`$VERSION-slim`, etc.).
   - Insert new version entry as first bullet under `vMAJOR.MINOR.x\n---`. If the section doesn't exist (new minor), create it above the previous minor.

   Entry format (trailing ` ,`):
   ```
   * $VERSION ([DD.MM.YYYY](https://github.com/osscontainertools/kaniko/releases/tag/$VERSION)): 
   [$VERSION](https://hub.docker.com/layers/martizih/kaniko/$VERSION/images/<digest>) , 
   [$VERSION-slim](.../$VERSION-slim/images/<digest-slim>) , 
   [$VERSION-debug](.../$VERSION-debug/images/<digest-debug>) , 
   [$VERSION-alpine](.../$VERSION-alpine/images/<digest-alpine>) , 
   [$VERSION-warmer](.../$VERSION-warmer/images/<digest-warmer>) , 
   [$VERSION-bootstrap](.../$VERSION-bootstrap/images/<digest-bootstrap>)
   ```

5. **Write result to `hub-sync.md`.**
