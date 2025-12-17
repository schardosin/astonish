import { Edit3 } from 'lucide-react'
import OverflowNode from './OverflowNode'

export default function InputNode({ data, selected }) {
  return (
    <OverflowNode 
      data={data} 
      selected={selected}
      icon={Edit3} 
      nodeType="Input"
      iconColor="#a78bfa"
    />
  )
}
