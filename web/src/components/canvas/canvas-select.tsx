import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";
import { CanvasPopover } from "./canvas-popover";
import "./canvas-select.css";

export interface CanvasSelectOption {
  value: string;
  label: string;
}

export interface CanvasSelectProps {
  label: string;
  value: string;
  options: readonly CanvasSelectOption[];
  onChange: (value: string) => void;
  disabled?: boolean;
  placeholder?: string;
  className?: string;
}

// Keep generation parameters in one visual and keyboard interaction pattern.
export function CanvasSelect({
  label,
  value,
  options,
  onChange,
  disabled = false,
  placeholder = "—",
  className = "",
}: CanvasSelectProps) {
  const id = useId();
  const anchor = useRef<HTMLButtonElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const search = useRef({ text: "", time: 0 });
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const [menuWidth, setMenuWidth] = useState(240);
  const selectedIndex = options.findIndex((option) => option.value === value);
  const selected = options[selectedIndex];
  const unavailable = disabled || options.length === 0;
  const currentIndex = Math.min(activeIndex, Math.max(0, options.length - 1));
  const optionValues = JSON.stringify(options.map((option) => option.value));
  const previousOptionValues = useRef(optionValues);

  function close(restoreFocus = true) {
    setOpen(false);
    search.current = { text: "", time: 0 };
    if (restoreFocus) anchor.current?.focus({ preventScroll: true });
  }

  function show(index?: number) {
    if (unavailable) return;
    setActiveIndex(index ?? (selectedIndex >= 0 ? selectedIndex : 0));
    setMenuWidth(Math.max(220, anchor.current?.offsetWidth ?? 0));
    search.current = { text: "", time: 0 };
    setOpen(true);
  }

  function choose(index: number) {
    const option = options[index];
    if (!option) return;
    close();
    if (option.value !== value) onChange(option.value);
  }

  function navigate(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "Tab") event.stopPropagation();
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        setActiveIndex((currentIndex + 1) % options.length);
        return;
      case "ArrowUp":
        event.preventDefault();
        setActiveIndex((currentIndex - 1 + options.length) % options.length);
        return;
      case "Home":
        event.preventDefault();
        setActiveIndex(0);
        return;
      case "End":
        event.preventDefault();
        setActiveIndex(options.length - 1);
        return;
      case "Enter":
      case " ":
        event.preventDefault();
        choose(currentIndex);
        return;
      case "Escape":
        event.preventDefault();
        close();
        return;
      case "Tab":
        // Return to the trigger before native Tab finds the next form control.
        close();
        return;
    }
    if (
      event.ctrlKey ||
      event.altKey ||
      event.metaKey ||
      event.key.length !== 1
    )
      return;
    event.preventDefault();
    const now = Date.now();
    const letter = event.key.toLocaleLowerCase();
    const previous = now - search.current.time < 700 ? search.current.text : "";
    const query = previous === letter ? letter : previous + letter;
    search.current = { text: query, time: now };
    for (let offset = 1; offset <= options.length; offset++) {
      const index = (currentIndex + offset) % options.length;
      if (options[index].label.toLocaleLowerCase().startsWith(query)) {
        setActiveIndex(index);
        break;
      }
    }
  }

  useEffect(() => {
    if (unavailable) setOpen(false);
  }, [unavailable]);

  useLayoutEffect(() => {
    const changed = previousOptionValues.current !== optionValues;
    previousOptionValues.current = optionValues;
    if (changed && open) close(!unavailable);
  }, [optionValues, open, unavailable]);

  useLayoutEffect(() => {
    if (!open || !list.current) return;
    const item = list.current.children[currentIndex] as HTMLElement | undefined;
    if (!item) return;
    const top = item.offsetTop;
    const bottom = top + item.offsetHeight;
    if (top < list.current.scrollTop) list.current.scrollTop = top;
    else if (bottom > list.current.scrollTop + list.current.clientHeight)
      list.current.scrollTop = bottom - list.current.clientHeight;
  }, [open, currentIndex]);

  return (
    <div
      className={`canvas-select ${className}`.trim()}
      data-canvas-no-zoom
      onPointerDown={(event) => event.stopPropagation()}
      onMouseDown={(event) => event.stopPropagation()}
      onClick={(event) => event.stopPropagation()}
      onDoubleClick={(event) => event.stopPropagation()}
      onWheel={(event) => event.stopPropagation()}
      onKeyDown={(event) => {
        if (event.key !== "Tab" && !(event.key === "Escape" && !open))
          event.stopPropagation();
      }}
    >
      <label id={`${id}-label`} htmlFor={`${id}-trigger`}>
        {label}
      </label>
      <button
        ref={anchor}
        id={`${id}-trigger`}
        type="button"
        role="combobox"
        className="canvas-select-trigger"
        aria-labelledby={`${id}-label`}
        aria-describedby={`${id}-value`}
        aria-expanded={open}
        aria-controls={open ? `${id}-list` : undefined}
        aria-haspopup="listbox"
        disabled={unavailable}
        title={`${label} · ${selected?.label ?? placeholder}`}
        onClick={() => (open ? close() : show())}
        onKeyDown={(event) => {
          if (event.key === "Tab" || (event.key === "Escape" && !open)) return;
          event.stopPropagation();
          if (event.key === "Escape") {
            event.preventDefault();
            close();
            return;
          }
          if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
            event.preventDefault();
            show(
              event.key === "Home"
                ? 0
                : event.key === "End"
                  ? options.length - 1
                  : undefined,
            );
          }
        }}
      >
        <span id={`${id}-value`} className="canvas-select-value">
          {selected?.label ?? placeholder}
        </span>
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="m4 6 4 4 4-4" />
        </svg>
      </button>
      {open && !unavailable && (
        <CanvasPopover
          anchor={anchor}
          width={menuWidth}
          className="canvas-select-menu"
          role="presentation"
          initialFocusSelector="[role='listbox']"
          onClose={() => close(false)}
        >
          <div
            ref={list}
            id={`${id}-list`}
            role="listbox"
            className="canvas-select-listbox"
            aria-label={label}
            aria-activedescendant={`${id}-option-${currentIndex}`}
            tabIndex={-1}
            onKeyDown={navigate}
          >
            {options.map((option, index) => (
              <div
                key={option.value}
                id={`${id}-option-${index}`}
                role="option"
                aria-selected={option.value === value}
                className="canvas-select-option"
                data-active={index === currentIndex || undefined}
                onPointerMove={() => setActiveIndex(index)}
                onPointerDown={(event) => event.preventDefault()}
                onClick={() => choose(index)}
              >
                <span>{option.label}</span>
                <svg viewBox="0 0 16 16" aria-hidden="true">
                  <path d="m3 8 3 3 7-7" />
                </svg>
              </div>
            ))}
          </div>
        </CanvasPopover>
      )}
    </div>
  );
}
