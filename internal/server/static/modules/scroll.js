const scrollFadeSelector = [
  ".table-wrap",
  ".detail-scroll",
  ".entry-dialog-scroll",
  ".entry-dialog pre"
].join(",");

export function bindNestedScrollChain(pre) {
  pre.addEventListener("wheel", (event) => {
    const parent = pre.closest(".entry-dialog-scroll");
    if (!parent || event.deltaY === 0) {
      return;
    }
    const maxScrollTop = pre.scrollHeight - pre.clientHeight;
    if (maxScrollTop <= 1) {
      return;
    }
    if (isWheelPointerOutside(pre, event)) {
      event.preventDefault();
      parent.scrollTop += normalizedWheelDeltaY(event);
      updateScrollFade(parent);
      return;
    }
    const atTop = pre.scrollTop <= 1;
    const atBottom = maxScrollTop - pre.scrollTop <= 1;
    if ((event.deltaY < 0 && atTop) || (event.deltaY > 0 && atBottom)) {
      event.preventDefault();
      parent.scrollTop += normalizedWheelDeltaY(event);
      updateScrollFade(parent);
    }
  }, { passive: false });
}

export function normalizedWheelDeltaY(event) {
  if (event.deltaMode === WheelEvent.DOM_DELTA_LINE) {
    return event.deltaY * 16;
  }
  if (event.deltaMode === WheelEvent.DOM_DELTA_PAGE) {
    return event.deltaY * window.innerHeight;
  }
  return event.deltaY;
}

export function isWheelPointerOutside(el, event) {
  const rect = el.getBoundingClientRect();
  return event.clientX < rect.left
    || event.clientX > rect.right
    || event.clientY < rect.top
    || event.clientY > rect.bottom;
}

export function updateScrollFade(el) {
  if (!el) {
    return;
  }
  const canScroll = el.scrollHeight - el.clientHeight > 1;
  const canScrollUp = canScroll && el.scrollTop > 1;
  const canScrollDown = canScroll && el.scrollHeight - el.scrollTop - el.clientHeight > 1;
  el.classList.toggle("scroll-fade-target", canScroll);
  el.classList.toggle("can-scroll-up", canScrollUp);
  el.classList.toggle("can-scroll-down", canScrollDown);
}

export function refreshScrollFades(root = document) {
  requestAnimationFrame(() => {
    root.querySelectorAll(scrollFadeSelector).forEach((el) => {
      if (el.dataset.scrollFadeBound !== "1") {
        el.dataset.scrollFadeBound = "1";
        el.addEventListener("scroll", () => updateScrollFade(el), { passive: true });
      }
      updateScrollFade(el);
    });
  });
}
