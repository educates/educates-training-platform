import * as vscode from "vscode"

import execWithIndices = require("regexp-match-indices")

export interface ReplaceMatchingTextParams {
    file: string,
    match: string,
    replacement: string,
    start?: number,
    stop?: number,
    isRegex?: boolean,
    group?: number,
    count?: number
}

export async function replaceMatchingText(params: ReplaceMatchingTextParams) {
    // Display the editor window for the target file.

    const doc = await vscode.workspace.openTextDocument(params.file)
    const editor = await vscode.window.showTextDocument(doc)

    // Bail out if there was no text to match provided.

    if (!params.match)
        return

    if (params.replacement === undefined)
        return

    // Find the matching text based on whether regex or exact match.

    const lines = editor.document.lineCount

    let startLine = (params.start === undefined || params.start === null) ? 0 : params.start
    let stopLine = (params.stop === undefined || params.stop === null) ? lines : params.stop

    if (startLine < 0)
        startLine = lines + startLine

    if (startLine < 0)
        startLine = 0
    else if (startLine >= lines)
        startLine = lines - 1

    if (stopLine < 0)
        stopLine = lines + stopLine

    if (stopLine < 0)
        stopLine = 0
    else if (stopLine >= lines)
        stopLine = lines - 1

    // Determine how many matches to replace. Default is 1 (first match
    // only). A value of -1 means replace all matches.

    let maxCount = (params.count === undefined || params.count === null || params.count === 0) ? 1 : params.count
    let replaceAll = maxCount < 0

    // Collect all matches to replace.

    let matches: { line: number, start: number, stop: number }[] = []

    if (params.isRegex) {
        let regex = new RegExp(params.match)
        let group = params.group || 0
        for (let line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let match = execWithIndices(regex, currentLine.text)
            if (match) {
                matches.push({
                    line,
                    start: match.indices[group][0],
                    stop: match.indices[group][1]
                })
                if (!replaceAll && matches.length >= maxCount)
                    break
            }
        }
    }
    else {
        for (let line = startLine; line < stopLine; line++) {
            let currentLine = editor.document.lineAt(line)
            let offset = currentLine.text.indexOf(params.match)
            if (offset >= 0) {
                matches.push({
                    line,
                    start: offset,
                    stop: offset + params.match.length
                })
                if (!replaceAll && matches.length >= maxCount)
                    break
            }
        }
    }

    // Bail out if there were no matches found anywhere in the file.

    if (matches.length === 0)
        return

    // Replace all matched text with the replacement text. Apply in
    // reverse order so line/column offsets remain valid.

    await editor.edit(builder => {
        for (let i = matches.length - 1; i >= 0; i--) {
            let m = matches[i]
            let startPosition = new vscode.Position(m.line, m.start)
            let stopPosition = new vscode.Position(m.line, m.stop)
            let range = new vscode.Range(startPosition, stopPosition)
            builder.replace(range, params.replacement)
        }
    })

    // Select and reveal the last replacement.

    let last = matches[matches.length - 1]
    let startPosition = new vscode.Position(last.line, last.start)
    let stopPosition = new vscode.Position(last.line, last.start + params.replacement.length)
    editor.selection = new vscode.Selection(startPosition, stopPosition)

    editor.revealRange(editor.selection, vscode.TextEditorRevealType.InCenter)

    await editor.document.save()
}
