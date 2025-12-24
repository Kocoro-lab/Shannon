import { AppLayout } from "@/components/app-layout";

export default function ProtectedLayout({
    children,
}: {
    children: React.ReactNode;
}) {
    return <AppLayout>{children}</AppLayout>;
}
