"use client";

import {
  createContext,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
} from "react";

type Theme = "system" | "light" | "dark";
const ThemeContext = createContext<{
  theme: Theme;
  setTheme: (value: Theme) => void;
}>({ theme: "system", setTheme: () => {} });

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>("system");
  const preference = useRef<Theme>("system");
  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    let initial: Theme = "system";
    try {
      const saved = localStorage.getItem("railvanta-theme");
      if (saved === "light" || saved === "dark") initial = saved;
    } catch {
      /* Browser storage can be disabled. */
    }
    // Preference is a browser-only setting, deliberately synchronized after hydration.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setThemeState(initial);
    preference.current = initial;
    const apply = () =>
      document.documentElement.classList.toggle(
        "dark",
        preference.current === "dark" ||
          (preference.current === "system" && media.matches),
      );
    apply();
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, []);
  function setTheme(value: Theme) {
    preference.current = value;
    setThemeState(value);
    try {
      localStorage.setItem("railvanta-theme", value);
    } catch {
      /* Keep the in-memory preference when storage is disabled. */
    }
    document.documentElement.classList.toggle(
      "dark",
      value === "dark" ||
        (value === "system" &&
          window.matchMedia("(prefers-color-scheme: dark)").matches),
    );
  }
  return (
    <ThemeContext.Provider value={{ theme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function ThemeSelect() {
  const { theme, setTheme } = useContext(ThemeContext);
  const id = useId();
  return (
    <div className="theme-control">
      <label htmlFor={id}>Theme</label>
      <select
        id={id}
        value={theme}
        onChange={(event) => setTheme(event.target.value as Theme)}
      >
        <option value="system">System</option>
        <option value="light">Light</option>
        <option value="dark">Dark</option>
      </select>
    </div>
  );
}
