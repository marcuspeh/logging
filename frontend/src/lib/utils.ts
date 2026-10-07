import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

// Tailwind-aware class joiner. Used everywhere the prototype's `cn`
// helper is referenced.
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}