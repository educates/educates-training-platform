import assert from "node:assert/strict";
import { test } from "node:test";
import { withServer } from "./scenario.mjs";

// The shape 'educates admin lookup kubeconfig' prints for a local cluster.
const CLI_KUBECONFIG = `apiVersion: v1
kind: Config
clusters:
- name: training-platform
  cluster:
    server: https://127.0.0.1:59223
    certificate-authority-data: Q0EgZGF0YQ==
contexts:
- name: training-platform
  context:
    cluster: training-platform
    user: remote-access
current-context: training-platform
users:
- name: remote-access
  user:
    token: a-token
`;

test("the remote cluster's kubeconfig names the server the lookup cluster reaches", () => {
  const rewritten = withServer(CLI_KUBECONFIG, "https://educates-lookup-b-control-plane:6443");
  assert.equal(rewritten, CLI_KUBECONFIG.replace("https://127.0.0.1:59223", "https://educates-lookup-b-control-plane:6443"));
});

test("a kubeconfig without a server address is refused", () => {
  assert.throws(
    () => withServer(CLI_KUBECONFIG.replace(/^ *server: .*\n/m, ""), "https://educates-lookup-b-control-plane:6443"),
    /names no server/,
  );
});

test("a kubeconfig with more than one server address is refused", () => {
  const two = CLI_KUBECONFIG.replace("contexts:", "- name: other\n  cluster:\n    server: https://10.0.0.1:6443\ncontexts:");
  assert.throws(() => withServer(two, "https://educates-lookup-b-control-plane:6443"), /names 2 servers/);
});
