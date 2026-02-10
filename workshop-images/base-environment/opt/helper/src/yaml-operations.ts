import * as fs from "fs"
import * as vscode from "vscode"
import * as yaml from "yaml"
import { Pair, YAMLMap, YAMLSeq } from "yaml/types"

const log_file_path = "/tmp/educates-vscode-helper.log"

function log(message: string) {
    fs.appendFileSync(log_file_path, message + "\n")
}

// --- Path parsing ---

// Attribute match segment: [key=value] in a path.
interface AttributeMatch {
    key: string
    value: string
}

type PathSegment = string | number | AttributeMatch

// Convert a dot-notation path string to an array of segments.
// Examples:
//   "spec.replicas" -> ["spec", "replicas"]
//   "spec.containers[0].name" -> ["spec", "containers", 0, "name"]
//   "spec.containers[name=nginx]" -> ["spec", "containers", {key:"name", value:"nginx"}]

function parsePathToSegments(path: string): PathSegment[] {
    const segments: PathSegment[] = []
    let i = 0
    while (i < path.length) {
        if (path[i] === ".") {
            i++
            continue
        }
        if (path[i] === "[") {
            const close = path.indexOf("]", i)
            if (close < 0) throw new Error("Unclosed bracket in path")
            const inner = path.substring(i + 1, close)
            const eq = inner.indexOf("=")
            if (eq >= 0) {
                segments.push({ key: inner.substring(0, eq), value: inner.substring(eq + 1) })
            } else {
                segments.push(parseInt(inner, 10))
            }
            i = close + 1
        } else {
            let end = i
            while (end < path.length && path[end] !== "." && path[end] !== "[") end++
            segments.push(path.substring(i, end))
            i = end
        }
    }
    return segments
}

// Resolve attribute match segments ([key=value]) to integer indices by
// inspecting the actual document. Returns a plain array suitable for
// doc.setIn() / doc.getIn() etc.

function resolvePathSegments(doc: yaml.Document.Parsed, segments: PathSegment[]): (string | number)[] {
    const resolved: (string | number)[] = []
    let current: any = doc.contents
    for (const seg of segments) {
        if (typeof seg === "string" || typeof seg === "number") {
            resolved.push(seg)
            if (current) {
                if (typeof current.getIn === "function") {
                    current = current.getIn([seg], true)
                } else {
                    current = undefined
                }
            }
        } else {
            // AttributeMatch — find the matching index in a sequence.
            if (!(current instanceof YAMLSeq)) {
                throw new Error(`Expected sequence for attribute match [${seg.key}=${seg.value}]`)
            }
            let found = false
            for (let idx = 0; idx < current.items.length; idx++) {
                const item = current.items[idx]
                if (item instanceof YAMLMap) {
                    const val = item.get(seg.key)
                    if (String(val) === seg.value) {
                        resolved.push(idx)
                        current = item
                        found = true
                        break
                    }
                }
            }
            if (!found) {
                throw new Error(`No item found matching [${seg.key}=${seg.value}]`)
            }
        }
    }
    return resolved
}

// --- Shared utilities ---

function showEditor(file: string): Thenable<vscode.TextEditor> {
    return vscode.workspace.openTextDocument(file)
        .then(doc => vscode.window.showTextDocument(doc))
}

function parseYamlDocument(text: string): yaml.Document.Parsed {
    return yaml.parseAllDocuments(text)[0]
}

// Replace the entire editor content with new text (preserves undo history).

async function replaceEditorContent(editor: vscode.TextEditor, newContent: string): Promise<void> {
    const doc = editor.document
    const entireRange = new vscode.Range(
        new vscode.Position(0, 0),
        new vscode.Position(doc.lineCount, 0)
    )
    await editor.edit(editBuilder => {
        editBuilder.replace(entireRange, newContent)
    })
    await doc.save()
}

// --- Handler: yaml-set ---

export interface YamlSetParams {
    file: string
    path: string
    value: any
}

export async function handleYamlSet(params: YamlSetParams): Promise<void> {
    log("Requesting yaml-set:")
    log(`  file = ${params.file}`)
    log(`  path = ${params.path}`)
    log(`  value = ${JSON.stringify(params.value)}`)

    const editor = await showEditor(params.file)
    const text = editor.document.getText()
    const doc = parseYamlDocument(text)
    const segments = parsePathToSegments(params.path)
    const resolved = resolvePathSegments(doc, segments)

    // doc.setIn(path, val) in yaml v1 navigates all path elements except
    // the last, then sets the last as a key. It throws if any intermediate
    // element doesn't exist. To handle missing intermediates, we find the
    // deepest existing prefix and wrap the remaining segments into nested
    // objects set as a single new key.
    //
    // Example: resolved=["spec","selector","matchLabels","app"], value="myapp"
    //   hasIn(["spec"]) = true  →  deepestExisting = 1
    //   hasIn(["spec","selector"]) = false  →  stop
    //   Wrap: i=3 {app:"myapp"}, i=2 {matchLabels:{app:"myapp"}}
    //         i=1 {selector:{matchLabels:{app:"myapp"}}}
    //   setIn(["spec"], {selector:{matchLabels:{app:"myapp"}}})
    //
    // But that would REPLACE the entire "spec" mapping! So instead we
    // need the setPath to include one new key beyond the existing prefix:
    //   setPath = ["spec","selector"]
    //   value = {matchLabels:{app:"myapp"}}
    // setIn navigates to "spec" (exists) and creates key "selector".

    log(`  resolved = ${JSON.stringify(resolved)}`)

    let deepestExisting = 0
    for (let i = 1; i <= resolved.length; i++) {
        const partial = resolved.slice(0, i)
        const exists = doc.hasIn(partial)
        log(`  hasIn(${JSON.stringify(partial)}) = ${exists}`)
        if (exists) {
            deepestExisting = i
        } else {
            break
        }
    }

    log(`  deepestExisting = ${deepestExisting} (of ${resolved.length})`)

    if (deepestExisting >= resolved.length - 1) {
        // The parent of the leaf key exists — setIn can handle this directly.
        log(`  setting directly at full path`)
        doc.setIn(resolved, params.value)
    } else {
        // Missing intermediate keys. setPath = existing prefix + one new key.
        // deepestExisting elements exist, so a path of deepestExisting+1
        // elements is safe (setIn navigates the first deepestExisting, then
        // creates the last one).
        // All remaining segments get wrapped into the value as nested objects.
        const setPath = resolved.slice(0, deepestExisting + 1)
        let nestedValue: any = params.value
        for (let i = resolved.length - 1; i >= deepestExisting + 1; i--) {
            nestedValue = { [resolved[i]]: nestedValue }
        }
        log(`  setPath = ${JSON.stringify(setPath)}`)
        log(`  nestedValue = ${JSON.stringify(nestedValue)}`)
        doc.setIn(setPath, nestedValue)
    }

    const newContent = doc.toString()
    await replaceEditorContent(editor, newContent)
}

// --- Handler: yaml-add-item ---

export interface YamlAddItemParams {
    file: string
    path: string
    value: any
}

export async function handleYamlAddItem(params: YamlAddItemParams): Promise<void> {
    log("Requesting yaml-add-item:")
    log(`  file = ${params.file}`)
    log(`  path = ${params.path}`)
    log(`  value = ${JSON.stringify(params.value)}`)

    const editor = await showEditor(params.file)
    const text = editor.document.getText()
    const doc = parseYamlDocument(text)
    const segments = parsePathToSegments(params.path)
    const resolved = resolvePathSegments(doc, segments)

    const node = doc.getIn(resolved, true)
    if (!(node instanceof YAMLSeq)) {
        throw new Error(`Path "${params.path}" does not point to a sequence`)
    }

    doc.addIn(resolved, params.value)

    const newContent = doc.toString()
    await replaceEditorContent(editor, newContent)
}

// --- Handler: yaml-insert-item ---

export interface YamlInsertItemParams {
    file: string
    path: string
    index: number
    value: any
}

export async function handleYamlInsertItem(params: YamlInsertItemParams): Promise<void> {
    log("Requesting yaml-insert-item:")
    log(`  file = ${params.file}`)
    log(`  path = ${params.path}`)
    log(`  index = ${params.index}`)
    log(`  value = ${JSON.stringify(params.value)}`)

    const editor = await showEditor(params.file)
    const text = editor.document.getText()
    const doc = parseYamlDocument(text)
    const segments = parsePathToSegments(params.path)
    const resolved = resolvePathSegments(doc, segments)

    const node = doc.getIn(resolved, true)
    if (!(node instanceof YAMLSeq)) {
        throw new Error(`Path "${params.path}" does not point to a sequence`)
    }

    const index = params.index || 0
    const newNode = yaml.createNode(params.value)
    node.items.splice(index, 0, newNode)

    const newContent = doc.toString()
    await replaceEditorContent(editor, newContent)
}

// --- Handler: yaml-replace-item ---

export interface YamlReplaceItemParams {
    file: string
    path: string
    value: any
}

export async function handleYamlReplaceItem(params: YamlReplaceItemParams): Promise<void> {
    log("Requesting yaml-replace-item:")
    log(`  file = ${params.file}`)
    log(`  path = ${params.path}`)
    log(`  value = ${JSON.stringify(params.value)}`)

    const editor = await showEditor(params.file)
    const text = editor.document.getText()
    const doc = parseYamlDocument(text)
    const segments = parsePathToSegments(params.path)
    const resolved = resolvePathSegments(doc, segments)

    // The path must point to an existing item (e.g., containers[0] or
    // containers[name=nginx]). setIn replaces the value at that path.

    if (!doc.hasIn(resolved)) {
        throw new Error(`Path "${params.path}" does not exist`)
    }

    doc.setIn(resolved, params.value)

    const newContent = doc.toString()
    await replaceEditorContent(editor, newContent)
}

// --- Handler: yaml-delete ---

export interface YamlDeleteParams {
    file: string
    path: string
}

export async function handleYamlDelete(params: YamlDeleteParams): Promise<void> {
    log("Requesting yaml-delete:")
    log(`  file = ${params.file}`)
    log(`  path = ${params.path}`)

    const editor = await showEditor(params.file)
    const text = editor.document.getText()
    const doc = parseYamlDocument(text)
    const segments = parsePathToSegments(params.path)
    const resolved = resolvePathSegments(doc, segments)

    if (!doc.hasIn(resolved)) {
        throw new Error(`Path "${params.path}" does not exist`)
    }

    doc.deleteIn(resolved)

    const newContent = doc.toString()
    await replaceEditorContent(editor, newContent)
}

// --- Handler: yaml-merge ---

export interface YamlMergeParams {
    file: string
    path: string
    value: any
}

export async function handleYamlMerge(params: YamlMergeParams): Promise<void> {
    log("Requesting yaml-merge:")
    log(`  file = ${params.file}`)
    log(`  path = ${params.path}`)
    log(`  value = ${JSON.stringify(params.value)}`)

    if (typeof params.value !== "object" || Array.isArray(params.value)) {
        throw new Error("Value for yaml-merge must be a mapping/object")
    }

    const editor = await showEditor(params.file)
    const text = editor.document.getText()
    const doc = parseYamlDocument(text)
    const segments = parsePathToSegments(params.path)
    const resolved = resolvePathSegments(doc, segments)

    const node = doc.getIn(resolved, true)
    if (!(node instanceof YAMLMap)) {
        throw new Error(`Path "${params.path}" does not point to a mapping`)
    }

    for (const [key, val] of Object.entries(params.value)) {
        doc.setIn([...resolved, key], val)
    }

    const newContent = doc.toString()
    await replaceEditorContent(editor, newContent)
}

// --- Handler: yaml-select ---

export interface YamlSelectParams {
    file: string
    path: string
}

export async function handleYamlSelect(params: YamlSelectParams): Promise<void> {
    log("Requesting yaml-select:")
    log(`  file = ${params.file}`)
    log(`  path = ${params.path}`)

    const editor = await showEditor(params.file)
    const text = editor.document.getText()
    const doc = parseYamlDocument(text)

    let startOffset: number = 0
    let endOffset: number = 0

    if (!params.path) {
        // Empty path — select entire document contents.
        const node = doc.contents
        if (node && node.range) {
            startOffset = node.range[0]
            endOffset = node.range[1]
        } else {
            startOffset = 0
            endOffset = text.length
        }
    } else {
        const segments = parsePathToSegments(params.path)
        const resolved = resolvePathSegments(doc, segments)

        // Navigate to the parent node so we can find the Pair (key+value)
        // for mapping entries, or the item node for sequence entries.
        const parentPath = resolved.slice(0, -1)
        const lastSegment = resolved[resolved.length - 1]
        const parentNode = parentPath.length === 0
            ? doc.contents
            : doc.getIn(parentPath, true)

        if (parentNode instanceof YAMLMap) {
            // Find the Pair whose key matches lastSegment, so we select
            // both the key and its value (e.g. "containers:\n- name: ...").
            let found = false
            for (const item of parentNode.items) {
                if (item instanceof Pair) {
                    const keyStr = item.key && typeof item.key === "object" && "value" in item.key
                        ? String((item.key as any).value)
                        : String(item.key)
                    if (keyStr === String(lastSegment)) {
                        const keyRange = (item.key as any)?.range
                        const valRange = (item.value as any)?.range
                        if (keyRange && valRange) {
                            startOffset = keyRange[0]
                            endOffset = valRange[1]
                        } else if (keyRange) {
                            startOffset = keyRange[0]
                            endOffset = keyRange[1]
                        } else {
                            throw new Error("Pair has no range information")
                        }
                        found = true
                        break
                    }
                }
            }
            if (!found) {
                throw new Error(`Key "${lastSegment}" not found in mapping`)
            }
        } else if (parentNode instanceof YAMLSeq) {
            const index = lastSegment as number
            const item = parentNode.items[index]
            if (!item) {
                throw new Error(`Index ${index} out of range`)
            }
            if ((item as any).range) {
                startOffset = (item as any).range[0]
                endOffset = (item as any).range[1]
            } else {
                throw new Error("Sequence item has no range information")
            }
        } else {
            throw new Error(`Parent of "${params.path}" is not a mapping or sequence`)
        }
    }

    // Trim trailing newlines/whitespace from the selection end.
    while (endOffset > startOffset && (text[endOffset - 1] === "\n" || text[endOffset - 1] === "\r")) {
        endOffset--
    }

    log(`  startOffset = ${startOffset}, endOffset = ${endOffset}`)

    const startPos = editor.document.positionAt(startOffset)
    const endPos = editor.document.positionAt(endOffset)

    editor.selection = new vscode.Selection(startPos, endPos)
    editor.revealRange(new vscode.Range(startPos, endPos), vscode.TextEditorRevealType.InCenter)
}
