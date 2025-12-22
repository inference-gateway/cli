"use client";

import { Moon, Sun } from "lucide-react";
import { useTheme } from "@/lib/theme-provider";
import { Button } from "@/components/ui/button";

export function ThemeToggle() {
  const { theme, toggleTheme } = useTheme();

  return (
    <Button
      variant="ghost"
      size="icon"
      onClick={toggleTheme}
      className="relative text-white/90 hover:text-white hover:bg-white/10 transition-colors"
      title={`Switch to ${theme === "light" ? "dark" : "light"} mode`}
    >
      {/* Show Sun in dark mode (to switch to light), Moon in light mode (to switch to dark) */}
      <Sun className="h-[1.2rem] w-[1.2rem] rotate-0 scale-0 transition-all dark:rotate-0 dark:scale-100" />
      <Moon className="absolute h-[1.2rem] w-[1.2rem] rotate-0 scale-100 transition-all dark:rotate-0 dark:scale-0" />
      <span className="sr-only">Toggle theme</span>
    </Button>
  );
}
