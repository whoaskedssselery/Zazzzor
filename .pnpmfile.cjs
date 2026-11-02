// TypeScript 7 (the native compiler) ships no classic JavaScript compiler
// API, which typescript-eslint and openapi-typescript are built on. This hook
// gives exactly those packages TypeScript 6 as a private dependency instead of
// resolving their `typescript` peer to the project's TypeScript 7; `tsc` in
// every package stays TypeScript 7. Drop it once they support TypeScript 7.
const TS_API_VERSION = "6.0.3";
const NEEDS_TS_API = [/^typescript-eslint$/, /^@typescript-eslint\//, /^ts-api-utils$/, /^openapi-typescript$/];

module.exports = {
  hooks: {
    readPackage(pkg) {
      if (pkg.peerDependencies?.typescript && NEEDS_TS_API.some((re) => re.test(pkg.name))) {
        delete pkg.peerDependencies.typescript;
        if (pkg.peerDependenciesMeta) delete pkg.peerDependenciesMeta.typescript;
        pkg.dependencies = { ...pkg.dependencies, typescript: TS_API_VERSION };
      }
      return pkg;
    },
  },
};
