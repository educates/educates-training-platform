// What a run does when it is interrupted or terminated, or its terminal
// goes away: it restores what it changed in the cluster, as a run that ends
// by itself does, and only then ends.

const SIGNALS = { SIGHUP: 1, SIGINT: 2, SIGTERM: 15 };

// once returns a function that runs fn the first time it is called and
// gives every caller that run's result, so that a step a run's end and a
// signal both ask for runs a single time.
export function once(fn) {
  let result = null;
  return () => (result ??= fn());
}

// onInterrupt runs restore when the process gets SIGINT, SIGTERM or SIGHUP
// and then exits with the signal's status, 128 plus its number. A second
// signal exits at once, leaving the restore unfinished. It returns the state
// of the interruption: the signal received, if any, and the promise of the
// exit.
export function onInterrupt(restore, { proc = process, log = console.error } = {}) {
  const state = { signal: null, exiting: null };
  for (const [signal, number] of Object.entries(SIGNALS)) {
    proc.on(signal, () => {
      if (state.signal) {
        log(`${signal} again: ending without waiting for the restore`);
        proc.exit(128 + number);
        return;
      }
      state.signal = signal;
      log(`${signal}: restoring what the run changed before ending; send it again to end at once`);
      state.exiting = Promise.resolve()
        .then(restore)
        .catch((e) => log(`Restoring what the run changed failed: ${e.message}`))
        .then(() => proc.exit(128 + number));
    });
  }
  return state;
}
