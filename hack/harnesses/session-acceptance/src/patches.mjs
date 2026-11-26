// JSON patches for the entries the harness adds to lists in the platform's
// custom resources, such as a theme in the SessionManager's spec.themes, and
// takes out again at the end of a run. Entries are known by their name. Each
// patch tests the value it was computed from first, so that it fails rather
// than overwrite a change someone made in between.

function segments(pointer) {
  return pointer.split("/").slice(1);
}

function pointerOf(parts) {
  return parts.map((p) => `/${p}`).join("");
}

function valueAt(obj, parts) {
  return parts.reduce((v, p) => (v === undefined || v === null ? undefined : v[p]), obj);
}

// setNamedEntry returns the patch that puts entry in the list at pointer,
// in place of the entry of the same name or appended.
export function setNamedEntry(obj, pointer, entry) {
  const parts = segments(pointer);
  const list = valueAt(obj, parts);
  if (list === undefined || list === null) {
    // Create the list under the nearest parent there, testing that parent.
    let there = parts.length - 1;
    while (there > 0 && (valueAt(obj, parts.slice(0, there)) ?? null) === null) there--;
    const nested = parts.slice(there + 1).reduceRight((value, part) => ({ [part]: value }), [entry]);
    return [
      { op: "test", path: pointerOf(parts.slice(0, there)), value: valueAt(obj, parts.slice(0, there)) },
      { op: "add", path: pointerOf(parts.slice(0, there + 1)), value: nested },
    ];
  }
  const index = list.findIndex((e) => e?.name === entry.name);
  const change =
    index < 0 ? { op: "add", path: `${pointer}/-`, value: entry } : { op: "replace", path: `${pointer}/${index}`, value: entry };
  return [{ op: "test", path: pointer, value: list }, change];
}

// removeNamedEntry returns the patch that takes the entry named name out of
// the list at pointer. When it was the only entry, the list goes too, and so
// does each parent below the top-level field, such as spec, that holds
// nothing else. It is empty when the list holds no such entry.
export function removeNamedEntry(obj, pointer, name) {
  const parts = segments(pointer);
  const list = valueAt(obj, parts);
  const index = Array.isArray(list) ? list.findIndex((e) => e?.name === name) : -1;
  if (index < 0) return [];
  if (list.length > 1) {
    return [
      { op: "test", path: pointer, value: list },
      { op: "remove", path: `${pointer}/${index}` },
    ];
  }
  let depth = parts.length;
  while (depth > 2 && Object.keys(valueAt(obj, parts.slice(0, depth - 1))).length === 1) depth--;
  const removed = pointerOf(parts.slice(0, depth));
  return [
    { op: "test", path: removed, value: valueAt(obj, parts.slice(0, depth)) },
    { op: "remove", path: removed },
  ];
}
