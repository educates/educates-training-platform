// The run's custom website theme. Its files are a Secret in a namespace of
// its own, which the SessionManager names in spec.themes, so that the
// session-manager chart's SecretCopier copies it into the session-manager's
// namespace. The test portal selects it, so that session-manager's
// SecretCopiers for the portal and for each workshop environment copy it on
// into the portal's namespace and into each environment's namespace, where
// the portal and the workshop dashboards serve it.

import { randomUUID } from "node:crypto";
import { patchResource, sessionManagerDeployment } from "./platform.mjs";
import { removeNamedEntry, setNamedEntry } from "./patches.mjs";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// The CSS custom property the theme's style sheets set to the run's marker,
// which a page shows it applied the theme by.
export const THEME_PROPERTY = "--session-acceptance-theme";

// The theme's files, as the session-manager's default theme names them. The
// portal's and the dashboard's style sheets set THEME_PROPERTY to marker;
// the other files are empty.
export function themeFiles(marker) {
  const css = `/* session-acceptance website theme, run ${marker} */\n:root { ${THEME_PROPERTY}: "${marker}"; }\n`;
  const files = {};
  for (const page of ["training-portal", "workshop-dashboard", "workshop-instructions"]) {
    for (const ext of ["html", "js", "css"]) files[`${page}.${ext}`] = "";
  }
  files["workshop-started.html"] = "";
  files["workshop-finished.html"] = "";
  files["training-portal.css"] = css;
  files["workshop-dashboard.css"] = css;
  return files;
}

// The paths, on the portal's host and on a session's host, of the style
// sheets the theme sets.
export const PORTAL_STYLESHEET = "/static/workshops/theme/training-portal.css";
export const DASHBOARD_STYLESHEET = "/static/theme/workshop-dashboard.css";

export class WebsiteTheme {
  constructor({ cluster, portal, log }) {
    this.cluster = cluster;
    this.log = log;
    // The namespace, the Secret, and the SessionManager's theme entry all
    // take this name; the copies keep it in the session-manager's namespace
    // and the portal's, and are named workshop-theme in an environment's.
    this.name = `${portal}-theme`;
    // Unique to the run, so that a copy an earlier run left can't pass.
    this.marker = `${portal}-${randomUUID()}`;
    this.copyNamespace = null;
  }

  describe() {
    return `Secret ${this.name}/${this.name}, named in the SessionManager's spec.themes and copied into ${this.copyNamespace ?? "the session-manager's namespace"}`;
  }

  // install creates the theme's Secret, names it in the SessionManager's
  // spec.themes, and waits for the session-manager chart's SecretCopier to
  // copy it, with this run's marker, into the session-manager's namespace.
  async install() {
    this.copyNamespace = (await sessionManagerDeployment(this.cluster)).namespace;
    this.log(`Creating the website theme ${this.name} in namespace ${this.name}`);
    const manifest = {
      apiVersion: "v1",
      kind: "List",
      items: [
        {
          apiVersion: "v1",
          kind: "Namespace",
          metadata: { name: this.name, labels: { "training.educates.dev/harness": "session-acceptance" } },
        },
        {
          apiVersion: "v1",
          kind: "Secret",
          metadata: { name: this.name, namespace: this.name },
          stringData: themeFiles(this.marker),
        },
      ],
    };
    const apply = await this.cluster.kubectl(["apply", "-f", "-"], { input: JSON.stringify(manifest) });
    if (apply.code !== 0) throw new Error(`creating the website theme ${this.name} failed: ${apply.stderr.trim()}`);

    this.log(`Naming the website theme ${this.name} in the SessionManager's spec.themes`);
    const entry = { name: this.name, source: { type: "Secret", secretRef: { name: this.name, namespace: this.name } } };
    await patchResource(this.cluster, ["sessionmanagers.platform.educates.dev", "cluster"], (sm) => setNamedEntry(sm, "/spec/themes", entry));

    this.log(`Waiting for the website theme to be copied into ${this.copyNamespace}`);
    const deadline = Date.now() + 5 * 60_000;
    while (Date.now() < deadline) {
      if ((await this.copiedCss()).includes(this.marker)) return;
      await sleep(5000);
    }
    throw new Error(`the website theme ${this.name} was not copied into ${this.copyNamespace} with this run's marker within 5 minutes`);
  }

  async copiedCss() {
    const r = await this.cluster.kubectl(["-n", this.copyNamespace, "get", "secret", this.name, "-o", "json"]);
    if (r.code !== 0) return "";
    const data = JSON.parse(r.stdout).data ?? {};
    return Buffer.from(data["training-portal.css"] ?? "", "base64").toString("utf8");
  }

  // remove takes the theme out of the SessionManager's spec.themes, which
  // removes the chart's SecretCopier and so its copy, and deletes the
  // theme's namespace.
  async remove() {
    const sm = await this.cluster.kubectlJson(["get", "sessionmanagers.platform.educates.dev", "cluster"]);
    if (removeNamedEntry(sm, "/spec/themes", this.name).length) {
      this.log(`Removing the website theme ${this.name} from the SessionManager's spec.themes`);
      await patchResource(this.cluster, ["sessionmanagers.platform.educates.dev", "cluster"], (now) => removeNamedEntry(now, "/spec/themes", this.name));
    }
    const ns = await this.cluster.kubectl(["get", "namespace", this.name, "--ignore-not-found", "-o", "name"]);
    if (ns.stdout.trim()) {
      this.log(`Deleting the website theme's namespace ${this.name}`);
      const r = await this.cluster.kubectl(["delete", "namespace", this.name, "--wait=true", "--timeout=300s"], { timeout: 310_000 });
      if (r.code !== 0) throw new Error(`deleting namespace ${this.name} failed: ${r.stderr.trim()}`);
    }
  }
}

// themeOutcome describes a checkTheme result in a phrase.
export function themeOutcome(t) {
  if (!t) return "not checked";
  if (t.error) return `could not be checked: ${t.error}`;
  if (!t.served) return `did not serve the theme's ${t.url} (HTTP ${t.status}${t.status === 200 ? ", without this run's marker" : ""})`;
  if (!t.applied) return "served the theme but its page did not apply it";
  return "served and applied the theme";
}

// checkTheme reports whether the page's origin serves the style sheet at
// path with the run's marker, and whether the page shows it applied, by
// THEME_PROPERTY on its root element.
export async function checkTheme(page, path, marker) {
  return page
    .evaluate(
      async ({ path, marker, property }) => {
        const r = await fetch(path, { cache: "no-store" });
        const text = await r.text();
        const applied = getComputedStyle(document.documentElement).getPropertyValue(property);
        return { url: path, status: r.status, served: r.ok && text.includes(marker), applied: applied.includes(marker) };
      },
      { path, marker, property: THEME_PROPERTY },
    )
    .catch((e) => ({ url: path, error: String(e).slice(0, 200), served: false, applied: false }));
}
