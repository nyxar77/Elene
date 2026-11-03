import { describe, expect, it } from "vitest";
import { validateAPK } from "./api";
describe("APK selection", () => {
  it("accepts an APK regardless of extension case", () =>
    expect(validateAPK({ name: "App.APK", size: 100 })).toBeNull());
  it.each([
    undefined,
    { name: "app.apks", size: 100 },
    { name: "app.apk", size: 0 },
    { name: "app.apk", size: 512 * 1024 * 1024 + 1 },
  ])("rejects missing, unsupported, empty and oversized uploads", (file) =>
    expect(validateAPK(file)).toBeTypeOf("string"),
  );
});
