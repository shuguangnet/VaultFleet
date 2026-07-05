import "@testing-library/jest-dom/vitest";

// antd 在 jsdom 环境下依赖这些 API，缺少会在组件挂载时报错。
if (typeof globalThis.ResizeObserver === "undefined") {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;
}

if (typeof globalThis.IntersectionObserver === "undefined") {
  class IntersectionObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() {
      return [];
    }
    root = null;
    rootMargin = "";
    thresholds = [];
  }
  globalThis.IntersectionObserver =
    IntersectionObserverStub as unknown as typeof IntersectionObserver;
}

if (typeof globalThis.matchMedia === "undefined") {
  globalThis.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}

if (typeof globalThis.scrollTo === "undefined") {
  globalThis.scrollTo = () => {};
}

if (typeof window !== "undefined") {
  if (typeof window.scrollTo === "undefined") {
    window.scrollTo = () => {};
  }
  if (typeof (window as any).matchMedia === "undefined") {
    (window as any).matchMedia = globalThis.matchMedia;
  }
}