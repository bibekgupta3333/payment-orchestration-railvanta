"use client";

import {
  useId,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";

export interface TabDefinition {
  id: string;
  label: string;
  content: ReactNode;
}

/** WAI-ARIA tabs: arrow keys, Home and End move between tabs. */
export function Tabs({
  label,
  tabs,
}: {
  label: string;
  tabs: TabDefinition[];
}) {
  const [selected, setSelected] = useState(tabs[0]?.id);
  const baseId = useId();
  const refs = useRef<Record<string, HTMLButtonElement | null>>({});
  const select = (index: number) => {
    const tab = tabs[(index + tabs.length) % tabs.length];
    setSelected(tab.id);
    refs.current[tab.id]?.focus();
  };
  const onKeyDown = (event: KeyboardEvent, index: number) => {
    const moves: Record<string, number> = {
      ArrowRight: index + 1,
      ArrowLeft: index - 1,
      Home: 0,
      End: tabs.length - 1,
    };
    if (event.key in moves) {
      event.preventDefault();
      select(moves[event.key]);
    }
  };
  return (
    <div className="tabs">
      <div role="tablist" aria-label={label} className="tab-list">
        {tabs.map((tab, index) => (
          <button
            key={tab.id}
            ref={(element) => {
              refs.current[tab.id] = element;
            }}
            type="button"
            role="tab"
            id={`${baseId}-${tab.id}-tab`}
            aria-selected={selected === tab.id}
            aria-controls={`${baseId}-${tab.id}-panel`}
            tabIndex={selected === tab.id ? 0 : -1}
            className="tab"
            onClick={() => setSelected(tab.id)}
            onKeyDown={(event) => onKeyDown(event, index)}
          >
            {tab.label}
          </button>
        ))}
      </div>
      {tabs.map((tab) => (
        <div
          key={tab.id}
          role="tabpanel"
          id={`${baseId}-${tab.id}-panel`}
          aria-labelledby={`${baseId}-${tab.id}-tab`}
          hidden={selected !== tab.id}
          tabIndex={0}
          className="tab-panel"
        >
          {selected === tab.id && tab.content}
        </div>
      ))}
    </div>
  );
}
