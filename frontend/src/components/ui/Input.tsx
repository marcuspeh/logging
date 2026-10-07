import React from "react";
import { cn } from "../../lib/utils";

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  icon?: React.ReactNode;
}

// Input primitive mirroring the prototype's chrome: optional leading
// icon, focus ring uses the brand blue, slate border at rest.
export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, icon, ...props }, ref) => {
    return (
      <div className="relative flex items-center w-full">
        {icon ? (
          <div className="absolute left-3 text-slate-400 pointer-events-none">
            {icon}
          </div>
        ) : null}
        <input
          ref={ref}
          className={cn(
            "flex h-9 w-full rounded-md border border-slate-300 bg-white px-3 py-1 text-sm text-slate-900 shadow-sm transition-colors file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-slate-400 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500/20 focus-visible:border-blue-500 disabled:cursor-not-allowed disabled:opacity-50",
            icon && "pl-9",
            className,
          )}
          {...props}
        />
      </div>
    );
  },
);
Input.displayName = "Input";