import sveltePreprocess from 'svelte-preprocess'

export default {
  // Consult https://github.com/sveltejs/svelte-preprocess
  // for more information about preprocessors
  preprocess: sveltePreprocess(),
  compilerOptions: {
    // Restores the legacy `$on`/`$set`/`$destroy` instance API on client
    // components so `@testing-library/svelte` tests can subscribe to
    // `createEventDispatcher` events via `component.$on(...)`.
    compatibility: {
      componentApi: 4
    }
  }
}
