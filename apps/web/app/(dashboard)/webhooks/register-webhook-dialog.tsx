"use client";

import { useState } from "react";
import { Check, Copy, TriangleAlert } from "lucide-react";
import { toast } from "sonner";
import { registerWebhookAction } from "./actions";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type DialogState =
  | { phase: "form"; url: string; error: string | null }
  | { phase: "secret"; id: string; url: string; secret: string; copied: boolean };

const INITIAL: DialogState = { phase: "form", url: "", error: null };

export function RegisterWebhookDialog() {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<DialogState>(INITIAL);

  function handleOpenChange(next: boolean) {
    setOpen(next);
    if (!next) setState(INITIAL);
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (state.phase !== "form") return;
    const url = state.url.trim();
    if (!url) {
      setState((s) => ({ ...s, error: "URL is required." }));
      return;
    }
    try {
      new URL(url);
    } catch {
      setState((s) => ({ ...s, error: "Enter a valid URL." }));
      return;
    }
    setState((s) => ({ ...s, error: null }));
    try {
      const data = await registerWebhookAction(url);
      setState({ phase: "secret", id: data.id, url: data.url, secret: data.secret, copied: false });
    } catch (err) {
      setState((s) => ({ ...s, error: err instanceof Error ? err.message : "Failed to register endpoint. Try again." }));
    }
  }

  function handleCopy() {
    if (state.phase !== "secret") return;
    navigator.clipboard.writeText(state.secret);
    setState((s) => ({ ...s, copied: true }));
    setTimeout(() => setState((s) => ({ ...s, copied: false })), 1500);
    toast.success("Secret copied");
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger asChild>
        <Button size="sm">Register endpoint</Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        {state.phase === "form" ? (
          <>
            <DialogHeader>
              <DialogTitle>Register webhook endpoint</DialogTitle>
              <DialogDescription>
                MeterBase will send signed HTTP POST requests to this URL when alerts fire.
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={handleSubmit} className="space-y-4 py-2">
              <div className="space-y-1.5">
                <Label htmlFor="url">Endpoint URL</Label>
                <Input
                  id="url"
                  type="url"
                  placeholder="https://hooks.example.com/meterbase"
                  value={state.url}
                  onChange={(e) =>
                    setState((s) => ({ ...s, url: e.target.value }))
                  }
                />
              </div>
              {state.error && (
                <p className="text-destructive text-sm">{state.error}</p>
              )}
              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => handleOpenChange(false)}
                >
                  Cancel
                </Button>
                <Button type="submit">Register endpoint</Button>
              </DialogFooter>
            </form>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Endpoint registered</DialogTitle>
              <DialogDescription>
                Save your signing secret now. It cannot be retrieved again.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-2">
              <div className="bg-muted border-border flex gap-3 rounded-lg border p-4">
                <TriangleAlert className="text-muted-foreground mt-0.5 size-4 shrink-0" />
                <p className="text-muted-foreground text-sm">
                  This secret is used to verify that webhook deliveries originate from
                  MeterBase. Copy it and store it securely — it will not be shown again.
                </p>
              </div>

              <div className="space-y-1.5">
                <Label>Signing secret</Label>
                <div className="flex items-center gap-2">
                  <code className="bg-muted flex-1 break-all rounded px-3 py-2 font-mono text-sm">
                    {state.secret}
                  </code>
                  <Button
                    type="button"
                    variant="outline"
                    size="icon"
                    onClick={handleCopy}
                    aria-label="Copy secret"
                  >
                    {state.copied ? (
                      <Check className="size-4" />
                    ) : (
                      <Copy className="size-4" />
                    )}
                  </Button>
                </div>
              </div>

              <div className="space-y-1.5">
                <Label>Endpoint ID</Label>
                <p className="text-muted-foreground font-mono text-xs">{state.id}</p>
              </div>
            </div>
            <DialogFooter>
              <Button onClick={() => handleOpenChange(false)}>Done</Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
