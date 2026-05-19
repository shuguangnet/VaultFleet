import { createBrowserRouter } from "react-router-dom";

export const router = createBrowserRouter([
  {
    path: "/",
    lazy: async () => {
      const { AuthGate } = await import("./pages/auth/auth-gate");
      return { Component: AuthGate };
    },
    children: [
      {
        index: true,
        lazy: async () => {
          const { DashboardPage } = await import("./pages/dashboard/dashboard-page");
          return { Component: DashboardPage };
        },
      },
      {
        path: "nodes",
        lazy: async () => {
          const { NodesPage } = await import("./pages/nodes/nodes-page");
          return { Component: NodesPage };
        },
      },
      {
        path: "nodes/:agentId",
        lazy: async () => {
          const { NodeDetailPage } = await import("./pages/nodes/node-detail-page");
          return { Component: NodeDetailPage };
        },
      },
      {
        path: "storage",
        lazy: async () => {
          const { StoragePage } = await import("./pages/storage/storage-page");
          return { Component: StoragePage };
        },
      },
      {
        path: "policies",
        lazy: async () => {
          const { PoliciesPage } = await import("./pages/policies/policies-page");
          return { Component: PoliciesPage };
        },
      },
      {
        path: "tasks",
        lazy: async () => {
          const { TasksPage } = await import("./pages/tasks/tasks-page");
          return { Component: TasksPage };
        },
      },
      {
        path: "snapshots",
        lazy: async () => {
          const { SnapshotsPage } = await import("./pages/snapshots/snapshots-page");
          return { Component: SnapshotsPage };
        },
      },
      {
        path: "notifications",
        lazy: async () => {
          const { NotificationsPage } = await import("./pages/notifications/notifications-page");
          return { Component: NotificationsPage };
        },
      },
      {
        path: "system",
        lazy: async () => {
          const { SystemPage } = await import("./pages/system/system-page");
          return { Component: SystemPage };
        },
      },
    ],
  },
  {
    path: "/login",
    lazy: async () => {
      const { AuthGate } = await import("./pages/auth/auth-gate");
      return { Component: AuthGate };
    },
  },
  {
    path: "/setup",
    lazy: async () => {
      const { AuthGate } = await import("./pages/auth/auth-gate");
      return { Component: AuthGate };
    },
  },
]);
