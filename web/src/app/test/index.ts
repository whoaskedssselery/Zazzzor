// The whole-app test harness. Test files of any layer may import it (they
// drive the app through its router); app code never does (eslint.config.js).
export { currentHref, renderApp, sessionHandler, testUser, type User } from "./render-app";
