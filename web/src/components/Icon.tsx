import {
  Activity,
  ArrowDownUp,
  ArrowLeft,
  ArrowRight,
  ArrowUpRight,
  Box,
  ChevronLast,
  Code,
  Copy,
  Crown,
  Eye,
  Flag,
  Folder,
  Globe,
  Grid2X2,
  Heart,
  History,
  KeyRound,
  Layers,
  Lock,
  LogOut,
  Menu,
  Moon,
  PanelLeftClose,
  Plus,
  Rocket,
  Search,
  Server,
  Settings2,
  Shield,
  Star,
  Sun,
  Terminal,
  Trash2,
  User,
  Users,
  Wrench,
  X,
  Zap,
  GitBranch,
} from "lucide-react";

const icons = {
  activity: Activity,
  arrow: ArrowUpRight,
  back: ArrowLeft,
  next: ArrowRight,
  box: Box,
  chevron: ArrowDownUp,
  code: Code,
  copy: Copy,
  crown: Crown,
  eye: Eye,
  flag: Flag,
  folder: Folder,
  globe: Globe,
  grid: Grid2X2,
  heart: Heart,
  history: History,
  key: KeyRound,
  layers: Layers,
  lock: Lock,
  logout: LogOut,
  menu: Menu,
  moon: Moon,
  sidebar: PanelLeftClose,
  plus: Plus,
  rocket: Rocket,
  search: Search,
  server: Server,
  settings: Settings2,
  shield: Shield,
  star: Star,
  sun: Sun,
  terminal: Terminal,
  trash: Trash2,
  user: User,
  users: Users,
  wrench: Wrench,
  close: X,
  bolt: Zap,
  branch: GitBranch,
  expand: ChevronLast,
};
export function Icon({ name }: { name: string }) {
  const Shape = icons[name as keyof typeof icons] || Box;
  return <Shape className="icon" strokeWidth={1.6} aria-hidden="true" />;
}
export function Brand() {
  return (
    <>
      <span className="mark">
        <Icon name="arrow" />
      </span>
      <span>Startship</span>
    </>
  );
}
