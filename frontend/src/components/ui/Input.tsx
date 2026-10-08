import React from "react";
import { X } from "lucide-react";
import { cn } from "../../lib/utils";

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  icon?: React.ReactNode;
  showClear?: boolean;
  onClear?: () => void;
}

export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, icon, showClear, onClear, value, ...props }, ref) => {
    return (
      <div className="relative flex items-center w-full">
        {icon ? (
          <div className="absolute left-3 text-slate-400 pointer-events-none">
            {icon}
          </div>
        ) : null}
        <input
          ref={ref}
          value={value}
          className={cn(
            "flex h-9 w-full rounded-md border border-slate-300 bg-white text-sm text-slate-900 shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-slate-400 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/20 focus-visible:border-blue-500 disabled:cursor-not-allowed disabled:opacity-50",
            icon ? "pl-9" : "pl-3",
            showClear ? "pr-8" : "pr-3",
            className,
          )}
          {...props}
        />
        {showClear && value !== undefined && String(value).length > 0 ? (
          <button
            type="button"
            onClick={onClear}
            className="absolute right-2 flex h-5 w-5 items-center justify-center rounded text-slate-400 hover:text-slate-600 transition-colors"
            tabIndex={-1}
            aria-label="Clear"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        ) : null}
      </div>
    );
  },
);
Input.displayName = "Input";