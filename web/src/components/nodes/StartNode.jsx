import { Play } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function StartNode({ id, data, selected }) {
  return (
    <OverflowNode 
      id={id}
      data={data} 
      selected={selected}
      icon={Play} 
      nodeType="Start"
      iconColor="#22c55e"
      hasTopHandle={false}
    />
  )
}
