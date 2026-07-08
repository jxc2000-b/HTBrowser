declare module 'react' {
  export type SetStateAction<S> = S | ((previous: S) => S);
  export type Dispatch<A> = (value: A) => void;

  export function useState<S>(initialState: S | (() => S)): [S, Dispatch<SetStateAction<S>>];

  export type RefObject<T> = {
    current: T;
  };

  export function useRef<T>(initialValue: T): RefObject<T>;

  export function useEffect(effect: () => void | (() => void), deps?: unknown[]): void;

  export type ChangeEvent<T = Element> = {
    target: T;
  };

  const React: {
    StrictMode: unknown;
  };

  export default React;
}

declare module 'react-dom/client' {
  export function createRoot(container: Element | DocumentFragment): {
    render(children: unknown): void;
  };
}

declare module 'react/jsx-runtime' {
  export const Fragment: unknown;
  export function jsx(type: unknown, props: unknown, key?: unknown): unknown;
  export function jsxs(type: unknown, props: unknown, key?: unknown): unknown;
}

declare namespace JSX {
  interface IntrinsicElements {
    [elementName: string]: any;
  }
}
