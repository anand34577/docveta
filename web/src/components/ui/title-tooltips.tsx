import * as React from "react";
import { createPortal } from "react-dom";

/**
 * Shows every `title` in the app as a styled tooltip instead of the browser's own: on mouse
 * hover after a short delay, and on keyboard focus. The attribute moves to data-tip so the
 * browser's tooltip never appears; elements with no other accessible name get it as aria-label.
 * ponytail: one global listener instead of wrapping ~100 call sites in <Tooltip>.
 */
export function TitleTooltips() {
  const [tip, setTip] = React.useState<{ text: string; rect: DOMRect } | null>(null);
  const box = React.useRef<HTMLDivElement>(null);
  const [pos, setPos] = React.useState<{ left: number; top: number } | null>(null);

  React.useEffect(() => {
    let current: Element | null = null;
    let timer = 0;
    const adopt = (target: EventTarget | null) => {
      const el = (target as Element | null)?.closest?.("[title], [data-tip]");
      if (!el || el.tagName === "IFRAME") return null;
      const t = el.getAttribute("title");
      if (t !== null) {
        el.removeAttribute("title");
        if (!t) return null;
        el.setAttribute("data-tip", t);
        if (!el.getAttribute("aria-label") && !el.getAttribute("aria-labelledby") && !el.textContent?.trim()) el.setAttribute("aria-label", t);
      }
      return el;
    };
    const hide = () => {
      window.clearTimeout(timer);
      current = null;
      setTip(null);
    };
    const show = (el: Element, delay: number) => {
      if (el === current) return;
      window.clearTimeout(timer);
      current = el;
      timer = window.setTimeout(() => {
        const text = el.getAttribute("data-tip");
        if (text && el.isConnected) setTip({ text, rect: el.getBoundingClientRect() });
      }, delay);
    };
    const over = (e: PointerEvent) => {
      const el = adopt(e.target);
      if (!el) return current && !current.contains(e.target as Node) && hide();
      if (e.pointerType === "mouse") show(el, 450);
    };
    const focus = (e: FocusEvent) => {
      const el = adopt(e.target);
      if (el && el === e.target && (e.target as Element).matches(":focus-visible")) show(el, 0);
    };
    document.addEventListener("pointerover", over);
    document.addEventListener("focusin", focus);
    document.addEventListener("focusout", hide);
    document.addEventListener("pointerdown", hide, true);
    document.addEventListener("keydown", hide, true);
    window.addEventListener("scroll", hide, true);
    return () => {
      document.removeEventListener("pointerover", over);
      document.removeEventListener("focusin", focus);
      document.removeEventListener("focusout", hide);
      document.removeEventListener("pointerdown", hide, true);
      document.removeEventListener("keydown", hide, true);
      window.removeEventListener("scroll", hide, true);
      window.clearTimeout(timer);
    };
  }, []);

  // Below the element, centred, kept on screen; above it when there's no room below.
  React.useLayoutEffect(() => {
    if (!tip || !box.current) return setPos(null);
    const { width, height } = box.current.getBoundingClientRect();
    const r = tip.rect;
    const left = Math.min(Math.max(8, r.left + r.width / 2 - width / 2), window.innerWidth - width - 8);
    const top = r.bottom + 6 + height > window.innerHeight - 8 ? r.top - 6 - height : r.bottom + 6;
    setPos({ left, top });
  }, [tip]);

  if (!tip) return null;
  return createPortal(
    <div ref={box} role="tooltip"
      style={{ left: pos?.left ?? -9999, top: pos?.top ?? 0 }}
      className="pointer-events-none fixed z-[100] max-w-72 rounded-md bg-fg px-2 py-1 text-xs leading-snug text-bg shadow-md animate-in">
      {tip.text}
    </div>,
    document.body,
  );
}
