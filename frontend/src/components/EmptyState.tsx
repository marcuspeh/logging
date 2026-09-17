interface Props {
  title: string;
  hint?: string;
}

export function EmptyState({ title, hint }: Props) {
  return (
    <div className="flex h-full flex-col items-center justify-center px-6 py-16 text-center">
      <p className="text-base font-medium text-slate-700">{title}</p>
      {hint ? <p className="mt-2 max-w-md text-sm text-slate-500">{hint}</p> : null}
    </div>
  );
}
