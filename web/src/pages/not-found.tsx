import { Link } from "@tanstack/react-router";
import { FileQuestion } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/misc";

export function NotFound() {
  return (
    <EmptyState icon={<FileQuestion />} title="Page not found" className="min-h-[60vh]" action={<Button asChild><Link to="/">Go home</Link></Button>}>
      The page you're looking for doesn't exist or you don't have access to it.
    </EmptyState>
  );
}
